#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Never inherit xtrace into a credential-bearing process. Buildkite's OIDC command registers
# its token with the job redactor, but the stronger rule is that neither the token nor the
# external-account document is ever passed through shell tracing or written to the checkout.
set +x
set -euo pipefail
umask 077

stage="${1:-}"
expectation="${2:-}"
case "${stage}:${expectation}" in
  build:allowed | build:denied | qualification:allowed | qualification:denied | promotion:allowed | promotion:denied) ;;
  *)
    echo "usage: $0 {build|qualification|promotion} {allowed|denied}" >&2
    exit 2
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/../.." && pwd -P)"
validator="${script_dir}/validate_wif_contract.py"
cd "${repo_root}"

for tool in buildkite-agent gcloud git python3; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "required WIF canary tool is unavailable: ${tool}" >&2
    exit 1
  fi
done

# The flags are newer than the generic OIDC defaults and all four are load-bearing. Fail on an
# old agent instead of falling back to the mutable compound subject or a provider-default aud.
oidc_help="$(buildkite-agent oidc request-token --help 2>&1)"
for flag in --audience --subject-claim --claim --format; do
  if [[ "${oidc_help}" != *"${flag}"* ]]; then
    echo "buildkite-agent lacks required OIDC option: ${flag}" >&2
    exit 1
  fi
done
unset oidc_help

mapfile -t runtime_values < <(
  python3 -B "${validator}" \
    --runtime-stage "${stage}" \
    --expectation "${expectation}" \
    --emit-runtime-values
)
if [[ "${#runtime_values[@]}" -ne 3 ]]; then
  echo "runtime validator did not return the exact WIF handoff" >&2
  exit 1
fi
readonly wif_audience="${runtime_values[0]}"
readonly provider_resource="${runtime_values[1]}"
readonly service_account="${runtime_values[2]}"
unset runtime_values

wif_tmp_root="${TMPDIR:-/tmp}"
if [[ "${wif_tmp_root}" != /* || ! -d "${wif_tmp_root}" ]]; then
  echo "refusing unsafe temporary root for WIF credentials" >&2
  exit 1
fi
wif_tmp="$(mktemp -d "${wif_tmp_root%/}/mindclade-buildkite-wif.XXXXXXXX")"
cleanup() {
  case "${wif_tmp:-}" in
    "${wif_tmp_root%/}"/mindclade-buildkite-wif.*)
      rm -rf -- "${wif_tmp}"
      ;;
    *)
      echo "refusing to remove unexpected WIF temporary path" >&2
      ;;
  esac
}
trap cleanup EXIT

readonly oidc_response="${wif_tmp}/oidc-response.json"
readonly credential_config="${wif_tmp}/external-account.json"
export CLOUDSDK_CONFIG="${wif_tmp}/gcloud"
export CLOUDSDK_CORE_DISABLE_PROMPTS=1
unset CLOUDSDK_AUTH_ACCESS_TOKEN
unset CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE
unset GOOGLE_APPLICATION_CREDENTIALS
unset GOOGLE_GHA_CREDS_PATH
mkdir -p "${CLOUDSDK_CONFIG}"

# `--format gcp` is a credential-source JSON document (`id_token`, not an external-account
# configuration). Generate the latter locally and bind it to exactly one target service
# account. No service-account key is created or consumed.
buildkite-agent oidc request-token \
  --audience "${wif_audience}" \
  --subject-claim pipeline_id \
  --claim organization_id \
  --format gcp >"${oidc_response}"

python3 -B - "${oidc_response}" <<'PY'
import json
import pathlib
import sys

value = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
if set(value) != {"id_token", "success", "token_type", "version"}:
    raise SystemExit("Buildkite returned an unexpected GCP OIDC response shape")
if value["success"] is not True or value["version"] != 1:
    raise SystemExit("Buildkite did not return a successful version-1 OIDC response")
if value["token_type"] != "urn:ietf:params:oauth:token-type:jwt":
    raise SystemExit("Buildkite returned an unexpected OIDC token type")
if not isinstance(value["id_token"], str) or value["id_token"].count(".") != 2:
    raise SystemExit("Buildkite OIDC response does not contain a JWT")
PY

gcloud iam workload-identity-pools create-cred-config "${provider_resource}" \
  --service-account="${service_account}" \
  --credential-source-file="${oidc_response}" \
  --credential-source-type=json \
  --credential-source-field-name=id_token \
  --output-file="${credential_config}" \
  --quiet >/dev/null

evidence_dir="${repo_root}/.buildkite-evidence"
evidence_path="${evidence_dir}/wif-${stage}-${expectation}.json"
mkdir -p "${evidence_dir}"

write_evidence() {
  local observed="$1"
  EVIDENCE_OBSERVED="${observed}" \
  EVIDENCE_EXPECTATION="${expectation}" \
  EVIDENCE_STAGE="${stage}" \
  EVIDENCE_PATH="${evidence_path}" \
  WIF_AUDIENCE="${wif_audience}" \
  WIF_SERVICE_ACCOUNT="${service_account}" \
    python3 -B - <<'PY'
import json
import os
import pathlib

names = (
    "BUILDKITE_AGENT_ID",
    "BUILDKITE_AGENT_META_DATA_QUEUE",
    "BUILDKITE_BRANCH",
    "BUILDKITE_BUILD_ID",
    "BUILDKITE_BUILD_NUMBER",
    "BUILDKITE_CLUSTER_ID",
    "BUILDKITE_COMPUTE_TYPE",
    "BUILDKITE_COMMIT",
    "BUILDKITE_COMMIT_RESOLVED",
    "BUILDKITE_GIT_COMMIT_VERIFICATION",
    "BUILDKITE_JOB_ID",
    "BUILDKITE_ORGANIZATION_ID",
    "BUILDKITE_ORGANIZATION_SLUG",
    "BUILDKITE_PIPELINE_ID",
    "BUILDKITE_PIPELINE_SLUG",
    "BUILDKITE_SOURCE",
    "BUILDKITE_STEP_ID",
    "BUILDKITE_STEP_KEY",
)
document = {
    "schema_version": 1,
    "stage": os.environ["EVIDENCE_STAGE"],
    "exchange_expected": os.environ["EVIDENCE_EXPECTATION"],
    "exchange_observed": os.environ["EVIDENCE_OBSERVED"],
    "wif_audience": os.environ["WIF_AUDIENCE"],
    "target_service_account": os.environ["WIF_SERVICE_ACCOUNT"],
    "buildkite": {name.removeprefix("BUILDKITE_").lower(): os.environ.get(name, "") for name in names},
    "credential_material_included": False,
}
path = pathlib.Path(os.environ["EVIDENCE_PATH"])
path.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
path.chmod(0o600)
PY
}

if [[ "${expectation}" == "denied" ]]; then
  # This job's key is deliberately absent from bootstrap's exact pipeline/step allowlist. An
  # accepted exchange means the provider or service-account binding is broader than source.
  # A generic CLI, network, or IAM failure is not proof: only STS's explicit provider attribute-
  # condition rejection demonstrates that the negative claim set reached the intended control.
  readonly negative_error="${wif_tmp}/negative-exchange-error.log"
  exchange_succeeded=false
  if gcloud auth login --cred-file="${credential_config}" --quiet >/dev/null 2>"${negative_error}"; then
    if gcloud auth print-access-token >/dev/null 2>>"${negative_error}"; then
      exchange_succeeded=true
    fi
  fi
  if [[ "${exchange_succeeded}" == "true" ]]; then
    write_evidence "unexpectedly_allowed"
    echo "untrusted Buildkite step unexpectedly exchanged a Google Cloud token" >&2
    exit 1
  fi
  if ! grep -Fqi "invalid_grant" "${negative_error}" \
    || ! grep -Fqi "attribute condition" "${negative_error}"; then
    write_evidence "indeterminate_failure"
    echo "untrusted ${stage} exchange failed without the exact STS attribute-condition rejection" >&2
    exit 1
  fi
  write_evidence "denied"
  echo "Observed the required STS attribute-condition denial for the untrusted ${stage} step."
  exit 0
fi

if ! gcloud auth login --cred-file="${credential_config}" --quiet >/dev/null 2>&1; then
  write_evidence "exchange_failed"
  echo "exact ${stage} WIF credential could not be activated" >&2
  exit 1
fi
if ! gcloud auth print-access-token >/dev/null 2>&1; then
  write_evidence "exchange_failed"
  echo "exact ${stage} identity could not mint a short-lived access token" >&2
  exit 1
fi

write_evidence "allowed"
echo "Observed the required positive exchange for the exact ${stage} identity."
