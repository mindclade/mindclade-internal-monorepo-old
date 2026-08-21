#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
IFS=$'\n\t'

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

resolve_runfile() {
  local relative_path="$1"
  [[ -n "${TEST_SRCDIR:-}" ]] || fail "Bazel runfiles root is unavailable"
  printf '%s/%s\n' "${TEST_SRCDIR}" "${relative_path}"
}

tool_marker="$(resolve_runfile "${MINDCLADE_VALIDATION_TOOL_MARKER_RLOCATION:?}")"
toolchain_manifest="$(resolve_runfile "${MINDCLADE_TOOLCHAIN_MANIFEST_RLOCATION:?}")"
workspace="$(resolve_runfile "${TEST_WORKSPACE:?}")"
policy_dir="${workspace}/infra/security/opa"
security_dir="${workspace}/infra/security"

for required in "${tool_marker}" "${toolchain_manifest}" "${policy_dir}/policy.rego"; do
  [[ -e "${required}" ]] || fail "declared runfile is missing: ${required}"
done

PATH="$(dirname "${tool_marker}"):/usr/bin:/bin"
export PATH

conftest verify --policy "${policy_dir}"
conftest test \
  "${security_dir}/audit-retention.yaml" \
  "${security_dir}/break-glass.yaml" \
  "${security_dir}/image-policy.yaml" \
  "${security_dir}/model-weight-access.yaml" \
  "${security_dir}/network-policies.yaml" \
  "${security_dir}/node-attestation.yaml" \
  "${security_dir}/pod-security.yaml" \
  "${security_dir}/secrets-rotation.yaml" \
  "${security_dir}/supply-chain-policy.yaml" \
  --namespace mindclade.infrastructure.security \
  --policy "${policy_dir}"
