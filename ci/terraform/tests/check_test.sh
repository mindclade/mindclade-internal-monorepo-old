#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
readonly CHECK="${SCRIPT_DIR}/../check.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
readonly REPO_ROOT
readonly FIXTURE_ROOT="${SCRIPT_DIR}/fixtures/repo"
readonly FAKE_TOOLS="${SCRIPT_DIR}/fixtures/tools"

expect_status() {
  local expected="$1"
  shift
  local actual
  set +e
  "$@" >/dev/null 2>&1
  actual=$?
  set -e
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'expected status %s, got %s: %s\n' "${expected}" "${actual}" "$*" >&2
    return 1
  fi
}

bash -n "${CHECK}"
if grep -Fq 'compgen' "${CHECK}"; then
  printf 'Terraform CI driver must not depend on Bash optional builtins absent from the Nix shell.\n' >&2
  exit 1
fi
grep -Fq 'infra/terraform/policy/test-policy.sh' "${CHECK}"
"${CHECK}" --help >/dev/null
"${CHECK}" contracts
expect_status 2 "${CHECK}"
expect_status 2 "${CHECK}" not-a-command
expect_status 2 "${CHECK}" plan-policy
expect_status 2 "${CHECK}" plan-policy --plan missing --profile missing --approval missing
expect_status 2 env MINDCLADE_TF_JOBS=0 "${CHECK}" contracts

test_tmp="$(mktemp -d "${TMPDIR:-/tmp}/mindclade-terraform-driver-test.XXXXXX")"
trap 'rm -rf -- "${test_tmp}"' EXIT

# Prove contracts compare both platform-package (`h1`) and registry-archive (`zh`) hashes.
# The isolated stdlib copy cannot mutate a developer's lock files or provider caches.
integrity_repo="${test_tmp}/integrity-repo"
python3 - "${REPO_ROOT}/infra/terraform" "${integrity_repo}/infra/terraform" <<'PY'
import pathlib
import shutil
import sys

source = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
shutil.copytree(
    source,
    destination,
    ignore=shutil.ignore_patterns(".terraform", "__pycache__", "*.pyc"),
)
PY
integrity_env=(
  env
  MINDCLADE_TF_TESTING=1
  MINDCLADE_TF_TEST_REPO_ROOT="${integrity_repo}"
  TF_PLUGIN_CACHE_DIR="${test_tmp}/integrity-plugin-cache"
)
"${integrity_env[@]}" "${CHECK}" contracts
python3 - "${integrity_repo}/infra/terraform/modules/storage/.terraform.lock.hcl" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
old = '    "zh:'
if old not in text:
    raise SystemExit("fixture lock has no zh checksum to mutate")
path.write_text(text.replace(old, '    "zh:mutated-', 1), encoding="utf-8")
PY
set +e
integrity_output="$("${integrity_env[@]}" "${CHECK}" contracts 2>&1)"
integrity_status=$?
set -e
[[ "${integrity_status}" == "1" ]]
grep -Fq "provider hashes differ from the canonical reviewed lock" <<<"${integrity_output}"

fixture_env=(
  env
  MINDCLADE_TF_TESTING=1
  MINDCLADE_TF_TEST_REPO_ROOT="${FIXTURE_ROOT}"
  MINDCLADE_TF_JOBS=2
  RUNNER_TEMP="${test_tmp}"
  TF_PLUGIN_CACHE_DIR="${test_tmp}/plugin-cache"
  PATH="${FAKE_TOOLS}:${PATH}"
)

expected_configurations="infra/terraform/environments/dns_hub
infra/terraform/modules/alpha
infra/terraform/modules/zeta"
actual_configurations="$("${fixture_env[@]}" "${CHECK}" __list configurations)"
[[ "${actual_configurations}" == "${expected_configurations}" ]]

expected_tests="infra/terraform/modules/zeta"
actual_tests="$("${fixture_env[@]}" "${CHECK}" __list tests)"
[[ "${actual_tests}" == "${expected_tests}" ]]

set +e
validation_output="$("${fixture_env[@]}" "${CHECK}" validate 2>&1)"
validation_status=$?
set -e
[[ "${validation_status}" == "1" ]]
grep -Fq "PASS validate infra/terraform/modules/alpha" <<<"${validation_output}"
grep -Fq "PASS validate infra/terraform/environments/dns_hub" <<<"${validation_output}"
grep -Fq "terraform validate failed" <<<"${validation_output}"
grep -Fq "synthetic validation failure" <<<"${validation_output}"

policy_output="$(
  "${fixture_env[@]}" "${CHECK}" plan-policy \
    --plan "${FIXTURE_ROOT}/plan.json" \
    --profile "${FIXTURE_ROOT}/profile.json"
)"
[[ "${policy_output}" == *"--plan ${FIXTURE_ROOT}/plan.json --profile ${FIXTURE_ROOT}/profile.json" ]]
[[ "${policy_output}" != *"--approval"* ]]

python3 "${SCRIPT_DIR}/workflow_test.py"

printf 'Terraform CI driver contract tests passed.\n'
