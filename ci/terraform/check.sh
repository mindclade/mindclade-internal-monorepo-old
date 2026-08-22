#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
if [[ "${MINDCLADE_TF_TESTING:-0}" == "1" && -n "${MINDCLADE_TF_TEST_REPO_ROOT:-}" ]]; then
  REPO_ROOT="$(cd "${MINDCLADE_TF_TEST_REPO_ROOT}" && pwd)"
else
  REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
fi
readonly REPO_ROOT
readonly TERRAFORM_ROOT="${REPO_ROOT}/infra/terraform"
readonly MODULE_ROOT="${TERRAFORM_ROOT}/modules"
readonly ENVIRONMENT_ROOT="${TERRAFORM_ROOT}/environments"
readonly COMPATIBILITY_CONFIG="${TERRAFORM_ROOT}/provider-compatibility.toml"

export CHECKPOINT_DISABLE=1
export TF_IN_AUTOMATION=1
export TF_INPUT=0

readonly MINDCLADE_TF_JOBS="${MINDCLADE_TF_JOBS:-4}"
if [[ ! "${MINDCLADE_TF_JOBS}" =~ ^[1-9][0-9]*$ ]]; then
  echo "MINDCLADE_TF_JOBS must be a positive integer" >&2
  exit 2
fi

readonly MINDCLADE_TF_TEMP_ROOT="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
export TF_PLUGIN_CACHE_DIR="${TF_PLUGIN_CACHE_DIR:-${MINDCLADE_TF_TEMP_ROOT%/}/mindclade-terraform-plugin-cache}"
mkdir -p "${TF_PLUGIN_CACHE_DIR}"

cd "${REPO_ROOT}"

usage() {
  cat <<'USAGE'
Usage: ci/terraform/check.sh <command> [options]

Commands:
  fmt          Check Terraform formatting.
  contracts    Check module layout, provider policy, and reviewed lock files.
  validate     Initialize configurations serially, then validate in parallel.
  lint         Run TFLint with bounded parallelism.
  security     Validate Trivy exceptions, scan IaC, and verify Conftest policies.
  test         Initialize missing suites serially, then run mock tests in parallel.
  docs         Run Terraform documentation and public-interface drift checks.
  compat       Test all published modules at minimum and reviewed providers.
  plan-policy  Evaluate a saved plan with explicit policy inputs.
  all          Run every repository-only gate in dependency order.

plan-policy options:
  --plan PATH      Saved `terraform show -json` document.
  --profile PATH   Approved policy profile.
  --approval PATH  Optional plan-digest-bound approval document.

Environment:
  MINDCLADE_TF_JOBS       Parallel validation/lint/test workers (default: 4).
  TF_PLUGIN_CACHE_DIR     Provider cache (defaults beneath runner/TMP temp storage).
  MINDCLADE_KEEP_TF_TMP   Set to 1 to retain compatibility workspaces for diagnosis.
USAGE
}

error() {
  printf '::error::%s\n' "$*" >&2
}

require_tool() {
  local tool="$1"
  if ! command -v "${tool}" >/dev/null 2>&1; then
    error "required tool is unavailable: ${tool}"
    return 1
  fi
}

compat_value() {
  local dotted_key="$1"
  python3 - "${COMPATIBILITY_CONFIG}" "${dotted_key}" <<'PY'
import pathlib
import sys
import tomllib

value = tomllib.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
for key in sys.argv[2].split("."):
    value = value[key]
if isinstance(value, list):
    print("\n".join(value))
else:
    print(value)
PY
}

configuration_dirs() {
  local dir
  for dir in "${MODULE_ROOT}"/* "${ENVIRONMENT_ROOT}"/*; do
    [[ -d "${dir}" ]] || continue
    has_tf_files "${dir}" || continue
    printf '%s\n' "${dir}"
  done | LC_ALL=C sort
}

has_tf_files() {
  local directory="$1" candidate
  for candidate in "${directory}"/*.tf; do
    [[ -f "${candidate}" ]] && return 0
  done
  return 1
}

has_test_files() {
  local directory="$1" candidate
  for candidate in "${directory}"/tests/*.tftest.hcl; do
    [[ -f "${candidate}" ]] && return 0
  done
  return 1
}

test_dirs() {
  local dir
  while IFS= read -r dir; do
    [[ -d "${dir}/tests" ]] || continue
    has_test_files "${dir}" || continue
    printf '%s\n' "${dir}"
  done < <(configuration_dirs)
}

compatibility_dirs() {
  local dir
  for dir in "${MODULE_ROOT}"/*; do
    [[ -d "${dir}" ]] || continue
    printf '%s\n' "${dir}"
  done | LC_ALL=C sort
}

relative_to_repo() {
  local path="$1"
  printf '%s\n' "${path#"${REPO_ROOT}/"}"
}

safe_log_name() {
  printf '%s' "$1" | sed 's/[^[:alnum:]_-]/_/g'
}

initialize_dirs() {
  local mode="$1"
  shift
  local dir output relative
  for dir in "$@"; do
    if [[ "${mode}" == "missing" && -d "${dir}/.terraform/providers" ]]; then
      continue
    fi
    relative="$(relative_to_repo "${dir}")"
    printf 'INIT %s\n' "${relative}"
    if ! output="$(terraform -chdir="${dir}" init \
      -backend=false \
      -input=false \
      -lockfile=readonly \
      -no-color 2>&1)"; then
      printf '::error file=%s/versions.tf::backendless terraform init failed\n' "${relative}" >&2
      printf '%s\n' "${output}" >&2
      return 1
    fi
  done
}

worker() {
  local phase="$1"
  local dir="$2"
  local name log status relative
  name="$(safe_log_name "${dir}")"
  log="${MINDCLADE_TF_LOG_DIR}/${name}.log"
  status="${MINDCLADE_TF_LOG_DIR}/${name}.status"
  relative="$(relative_to_repo "${dir}")"

  set +e
  case "${phase}" in
    validate)
      terraform -chdir="${dir}" validate -no-color >"${log}" 2>&1
      ;;
    lint)
      tflint --chdir="${dir}" --format=compact >"${log}" 2>&1
      ;;
    test)
      if [[ -f "${dir}/terraform.tfvars.example" ]]; then
        terraform -chdir="${dir}" test -no-color \
          -var-file=terraform.tfvars.example >"${log}" 2>&1
      else
        terraform -chdir="${dir}" test -no-color >"${log}" 2>&1
      fi
      ;;
    *)
      printf 'unknown worker phase: %s\n' "${phase}" >"${log}"
      false
      ;;
  esac
  local result=$?
  set -e

  if (( result == 0 )); then
    printf 'pass\n' >"${status}"
    printf 'PASS %s %s\n' "${phase}" "${relative}"
    return 0
  fi
  printf 'fail\n' >"${status}"
  return "${result}"
}

run_parallel() {
  local phase="$1"
  shift
  local dirs=("$@")
  local log_dir xargs_status dir name relative
  if (( ${#dirs[@]} == 0 )); then
    error "${phase} discovered no Terraform configurations"
    return 1
  fi

  log_dir="$(mktemp -d "${MINDCLADE_TF_TEMP_ROOT%/}/mindclade-terraform-${phase}.XXXXXX")"
  export MINDCLADE_TF_LOG_DIR="${log_dir}"

  set +e
  printf '%s\0' "${dirs[@]}" | xargs -0 -n 1 -P "${MINDCLADE_TF_JOBS}" \
    "${BASH}" "${SCRIPT_DIR}/check.sh" __worker "${phase}"
  xargs_status=$?
  set -e

  if (( xargs_status == 0 )); then
    case "${log_dir}" in
      "${MINDCLADE_TF_TEMP_ROOT%/}"/mindclade-terraform-"${phase}".*)
        rm -rf -- "${log_dir}"
        ;;
      *)
        error "refusing to clean unexpected successful-check log directory: ${log_dir}"
        return 1
        ;;
    esac
    return 0
  fi

  for dir in "${dirs[@]}"; do
    name="$(safe_log_name "${dir}")"
    [[ -f "${log_dir}/${name}.status" ]] || continue
    [[ "$(<"${log_dir}/${name}.status")" == "fail" ]] || continue
    relative="$(relative_to_repo "${dir}")"
    printf '::error file=%s/versions.tf::terraform %s failed\n' "${relative}" "${phase}" >&2
    cat "${log_dir}/${name}.log" >&2
  done
  printf 'Terraform %s logs: %s\n' "${phase}" "${log_dir}" >&2
  return 1
}

run_fmt() {
  require_tool terraform
  terraform fmt -check -recursive infra/terraform
}

run_contracts() {
  require_tool python3
  local failed=0 dir file reviewed constraint lock_path fixture version h1_count

  if ! python3 - "${COMPATIBILITY_CONFIG}" "${TERRAFORM_ROOT}" <<'PY'
import pathlib
import re
import sys
import tomllib

config_path = pathlib.Path(sys.argv[1])
root = pathlib.Path(sys.argv[2])
try:
    google = tomllib.loads(config_path.read_text(encoding="utf-8"))["google"]
except (OSError, KeyError, tomllib.TOMLDecodeError) as exc:
    raise SystemExit(f"invalid provider compatibility policy: {exc}")

required = {"source", "constraint", "minimum", "reviewed", "platforms", "locks"}
missing = sorted(required - google.keys())
if missing:
    raise SystemExit(f"provider compatibility policy is missing: {', '.join(missing)}")
if google["source"] != "registry.terraform.io/hashicorp/google":
    raise SystemExit("provider compatibility policy must govern hashicorp/google")
if google["constraint"] != ">= 7.41.0, < 8.0.0":
    raise SystemExit("provider compatibility constraint drifted from the published contract")
for field in ("minimum", "reviewed"):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", google[field]):
        raise SystemExit(f"google.{field} must be an exact semantic version")
if google["minimum"] != "7.41.0" or google["reviewed"] != "7.45.0":
    raise SystemExit("minimum/reviewed provider versions require an explicit compatibility-policy update")
if google["platforms"] != ["linux_amd64", "linux_arm64", "darwin_arm64"]:
    raise SystemExit("provider lock platforms must match the Nix-supported host systems")
for lock in google["locks"].values():
    candidate = root / lock
    if not candidate.is_file() or not candidate.resolve().is_relative_to(root.resolve()):
        raise SystemExit(f"provider lock fixture is missing or escapes Terraform root: {lock}")
PY
  then
    error "provider compatibility policy is invalid"
    failed=1
  fi

  reviewed="$(compat_value google.reviewed)"
  constraint="$(compat_value google.constraint)"
  fixture="${TERRAFORM_ROOT}/$(compat_value google.locks.reviewed)"

  for dir in "${MODULE_ROOT}"/*; do
    [[ -d "${dir}" ]] || continue
    for file in README.md main.tf outputs.tf variables.tf versions.tf .terraform.lock.hcl; do
      if [[ ! -f "${dir}/${file}" ]]; then
        printf '::error file=%s/%s::required Terraform module contract file is missing\n' \
          "$(relative_to_repo "${dir}")" "${file}" >&2
        failed=1
      fi
    done
    if [[ ! -d "${dir}/tests" ]] || ! has_test_files "${dir}"; then
      printf '::error file=%s::materialized Terraform modules require a mock test suite\n' \
        "$(relative_to_repo "${dir}")" >&2
      failed=1
    fi
  done

  while IFS= read -r dir; do
    [[ "${dir}" == "${MODULE_ROOT}"/* ]] || continue
    if ! grep -Eq 'version[[:space:]]*=[[:space:]]*">= 7\.41\.0, < 8\.0\.0"' "${dir}/versions.tf"; then
      printf '::error file=%s/versions.tf::Google provider constraint must be %s\n' \
        "$(relative_to_repo "${dir}")" "${constraint}" >&2
      failed=1
    fi

    lock_path="${dir}/.terraform.lock.hcl"
    [[ -f "${lock_path}" ]] || continue
    version="$(sed -n 's/^  version     = "\([^"]*\)"/\1/p' "${lock_path}")"
    if [[ "${version}" != "${reviewed}" ]]; then
      printf '::error file=%s/.terraform.lock.hcl::reviewed Google provider lock must be %s\n' \
        "$(relative_to_repo "${dir}")" "${reviewed}" >&2
      failed=1
    fi
    if ! grep -Fq "constraints = \"${constraint}\"" "${lock_path}"; then
      printf '::error file=%s/.terraform.lock.hcl::provider lock constraint drifted\n' \
        "$(relative_to_repo "${dir}")" >&2
      failed=1
    fi
    if ! diff -u \
      <(grep -E '^    "(h1|zh):' "${fixture}") \
      <(grep -E '^    "(h1|zh):' "${lock_path}") >/dev/null; then
      printf '::error file=%s/.terraform.lock.hcl::provider hashes differ from the canonical reviewed lock\n' \
        "$(relative_to_repo "${dir}")" >&2
      failed=1
    fi
  done < <(configuration_dirs)

  for lock_path in \
    "${TERRAFORM_ROOT}/$(compat_value google.locks.minimum)" \
    "${TERRAFORM_ROOT}/$(compat_value google.locks.reviewed)"; do
    h1_count="$(grep -c '^    "h1:' "${lock_path}" || true)"
    if [[ "${h1_count}" != "3" ]]; then
      printf '::error file=%s::canonical lock must contain one h1 checksum per supported platform\n' \
        "$(relative_to_repo "${lock_path}")" >&2
      failed=1
    fi
  done

  return "${failed}"
}

run_validate() {
  require_tool terraform
  local dirs=()
  while IFS= read -r dir; do dirs+=("${dir}"); done < <(configuration_dirs)
  initialize_dirs always "${dirs[@]}"
  run_parallel validate "${dirs[@]}"
}

run_lint() {
  require_tool tflint
  local dirs=() dir
  while IFS= read -r dir; do dirs+=("${dir}"); done < <(configuration_dirs)
  run_parallel lint "${dirs[@]}"
}

run_security() {
  require_tool python3
  require_tool trivy
  require_tool conftest

  python3 <<'PY'
import datetime as dt
import re
import subprocess
from pathlib import Path

result = subprocess.run(
    ["git", "grep", "-n", "#trivy:ignore:", "--", "infra/terraform"],
    check=False,
    text=True,
    stdout=subprocess.PIPE,
)
if result.returncode not in (0, 1):
    raise SystemExit(result.returncode)

pattern = re.compile(r"#trivy:ignore:(?:AVD-)?GCP-[0-9]{4}:exp:([0-9]{4}-[0-9]{2}-[0-9]{2})$")
today = dt.date.today()
maximum = dt.date.today() + dt.timedelta(days=366)
failed = False
for line in result.stdout.splitlines():
    match = pattern.search(line)
    if not match:
        print(f"::error::Trivy exceptions require one exact GCP check ID and an expiry date: {line}")
        failed = True
        continue
    try:
        expiry = dt.date.fromisoformat(match.group(1))
    except ValueError:
        print(f"::error::Trivy exception has an invalid expiry date: {line}")
        failed = True
        continue
    if expiry < today:
        print(f"::error::Trivy exception is expired: {line}")
        failed = True
    if expiry > maximum:
        print(f"::error::Trivy exception exceeds the 366-day review horizon: {line}")
        failed = True

module_ignore = Path("infra/terraform/policy/trivy-bazel-remote-execution-ignore.yaml")
if not module_ignore.is_file():
    print(f"::error file={module_ignore}::module-scoped Trivy exception file is missing")
    failed = True
else:
    ignore_text = module_ignore.read_text(encoding="utf-8")
    ids = re.findall(r"^\s+- id: (\S+)\s*$", ignore_text, flags=re.MULTILINE)
    expiries = re.findall(r"^\s+expired_at: (\S+)\s*$", ignore_text, flags=re.MULTILINE)
    if ids != ["GCP-0078"]:
        print(f"::error file={module_ignore}::expected exactly the scoped GCP-0078 exception")
        failed = True
    if "Owner:" not in ignore_text or "Reason:" not in ignore_text:
        print(f"::error file={module_ignore}::exception statement requires Owner and Reason metadata")
        failed = True
    if len(expiries) != 1:
        print(f"::error file={module_ignore}::exception requires exactly one expiry date")
        failed = True
    else:
        try:
            expiry = dt.date.fromisoformat(expiries[0])
        except ValueError:
            print(f"::error file={module_ignore}::exception has an invalid expiry date")
            failed = True
        else:
            if expiry < today:
                print(f"::error file={module_ignore}::exception is expired")
                failed = True
            if expiry > maximum:
                print(f"::error file={module_ignore}::exception exceeds the 366-day review horizon")
                failed = True
if failed:
    raise SystemExit(1)
PY

  # Trivy 0.74.0's embedded GCP-0078 check emits an issue without source-location metadata
  # for this IAM-only module, so an inline resource suppression cannot match it. Keep the
  # exception scoped to a separate invocation: every other check still evaluates the module,
  # and GCP-0078 remains enforced everywhere that creates a bucket.
  trivy config \
    --disable-telemetry \
    --exit-code 1 \
    --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL \
    --skip-check-update \
    --skip-dirs '**/.terraform' \
    --skip-dirs 'infra/terraform/modules/bazel_remote_execution' \
    infra/terraform

  trivy config \
    --disable-telemetry \
    --exit-code 1 \
    --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL \
    --skip-check-update \
    --ignorefile infra/terraform/policy/trivy-bazel-remote-execution-ignore.yaml \
    infra/terraform/modules/bazel_remote_execution

  local policy_test="infra/terraform/policy/test-policy.sh"
  if [[ ! -x "${policy_test}" ]]; then
    error "Terraform Conftest policy fixture gate is missing or not executable: ${policy_test}"
    return 1
  fi
  "${policy_test}"
}

run_test() {
  require_tool terraform
  local dirs=() dir
  while IFS= read -r dir; do dirs+=("${dir}"); done < <(test_dirs)
  initialize_dirs missing "${dirs[@]}"
  run_parallel test "${dirs[@]}"
  printf 'Ran %d Terraform test suite(s).\n' "${#dirs[@]}"
}

run_docs() {
  local checker="infra/terraform/governance/check.sh"
  if [[ ! -x "${checker}" ]]; then
    error "Terraform documentation/interface checker is missing or not executable: ${checker}"
    return 1
  fi
  "${checker}"
}

cleanup_compatibility_workspace() {
  local workspace="$1"
  if [[ "${MINDCLADE_KEEP_TF_TMP:-0}" == "1" ]]; then
    printf 'Retained Terraform compatibility workspace: %s\n' "${workspace}"
    return
  fi
  case "${workspace}" in
    "${MINDCLADE_TF_TEMP_ROOT%/}"/mindclade-terraform-compat.*)
      rm -rf -- "${workspace}"
      ;;
    *)
      error "refusing to clean unexpected compatibility workspace: ${workspace}"
      ;;
  esac
}

run_compatibility_version() {
  local label="$1"
  local version="$2"
  local fixture="$3"
  local workspace copied_root dir relative
  local dirs=() suites=()

  workspace="$(mktemp -d "${MINDCLADE_TF_TEMP_ROOT%/}/mindclade-terraform-compat.XXXXXX")"
  copied_root="${workspace}/infra/terraform"
  python3 - "${TERRAFORM_ROOT}" "${copied_root}" <<'PY'
import pathlib
import shutil
import sys

source = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])

def ignore(_directory: str, names: list[str]) -> set[str]:
    return {name for name in names if name in {".terraform", ".terraform.lock.hcl"}}

shutil.copytree(source, destination, ignore=ignore)
PY

  while IFS= read -r dir; do
    relative="$(relative_to_repo "${dir}")"
    dir="${workspace}/${relative}"
    cp "${fixture}" "${dir}/.terraform.lock.hcl"
    dirs+=("${dir}")
    if [[ -d "${dir}/tests" ]] && has_test_files "${dir}"; then
      suites+=("${dir}")
    fi
  done < <(compatibility_dirs)

  printf 'Provider compatibility: %s (%s), %d configuration(s)\n' \
    "${label}" "${version}" "${#dirs[@]}"
  if ! initialize_dirs always "${dirs[@]}" || \
    ! run_parallel validate "${dirs[@]}" || \
    ! run_parallel test "${suites[@]}"; then
    printf 'Compatibility workspace retained for failure diagnosis: %s\n' "${workspace}" >&2
    return 1
  fi
  cleanup_compatibility_workspace "${workspace}"
}

run_compat() {
  require_tool python3
  require_tool terraform
  run_contracts

  local minimum reviewed minimum_lock reviewed_lock
  minimum="$(compat_value google.minimum)"
  reviewed="$(compat_value google.reviewed)"
  minimum_lock="${TERRAFORM_ROOT}/$(compat_value google.locks.minimum)"
  reviewed_lock="${TERRAFORM_ROOT}/$(compat_value google.locks.reviewed)"

  run_compatibility_version minimum "${minimum}" "${minimum_lock}"
  run_compatibility_version reviewed "${reviewed}" "${reviewed_lock}"
}

run_plan_policy() {
  local plan="" profile="" approval=""
  while (( $# > 0 )); do
    case "$1" in
      --plan|--profile|--approval)
        if (( $# < 2 )); then
          error "$1 requires a path"
          return 2
        fi
        case "$1" in
          --plan) plan="$2" ;;
          --profile) profile="$2" ;;
          --approval) approval="$2" ;;
        esac
        shift 2
        ;;
      *)
        error "unknown plan-policy option: $1"
        return 2
        ;;
    esac
  done

  if [[ -z "${plan}" || -z "${profile}" ]]; then
    error "plan-policy requires --plan and --profile"
    return 2
  fi
  for input in "${plan}" "${profile}"; do
    if [[ ! -f "${input}" ]]; then
      error "plan-policy input does not exist: ${input}"
      return 2
    fi
  done
  if [[ -n "${approval}" && ! -f "${approval}" ]]; then
    error "plan-policy input does not exist: ${approval}"
    return 2
  fi

  local policy_args=(--plan "${plan}" --profile "${profile}")
  if [[ -n "${approval}" ]]; then
    policy_args+=(--approval "${approval}")
  fi

  if [[ -x infra/terraform/policy/check-plan.sh ]]; then
    infra/terraform/policy/check-plan.sh "${policy_args[@]}"
  elif [[ -f infra/terraform/policy/check-plan.py ]]; then
    python3 infra/terraform/policy/check-plan.py "${policy_args[@]}"
  else
    error "saved-plan policy driver is missing"
    return 1
  fi
}

run_all() {
  run_fmt
  run_contracts
  run_validate
  run_lint
  run_security
  run_test
  run_docs
  run_compat
}

command="${1:-}"
if (( $# > 0 )); then shift; fi
case "${command}" in
  fmt) run_fmt "$@" ;;
  contracts) run_contracts "$@" ;;
  validate) run_validate "$@" ;;
  lint) run_lint "$@" ;;
  security) run_security "$@" ;;
  test) run_test "$@" ;;
  docs) run_docs "$@" ;;
  compat) run_compat "$@" ;;
  plan-policy) run_plan_policy "$@" ;;
  all) run_all "$@" ;;
  __worker) worker "$@" ;;
  __list)
    case "${1:-}" in
      configurations) configuration_dirs | while IFS= read -r path; do relative_to_repo "${path}"; done ;;
      tests) test_dirs | while IFS= read -r path; do relative_to_repo "${path}"; done ;;
      compatibility) compatibility_dirs | while IFS= read -r path; do relative_to_repo "${path}"; done ;;
      *) error "unknown discovery set: ${1:-}"; exit 2 ;;
    esac
    ;;
  -h|--help) usage ;;
  '') usage >&2; exit 2 ;;
  *) error "unknown Terraform check command: ${command}"; usage >&2; exit 2 ;;
esac
