#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

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

archive="$(resolve_runfile "${MINDCLADE_VALIDATION_ARCHIVE_RLOCATION:?}")"
extractor="$(resolve_runfile "${MINDCLADE_VALIDATION_EXTRACTOR_RLOCATION:?}")"
tool_marker="$(resolve_runfile "${MINDCLADE_VALIDATION_TOOL_MARKER_RLOCATION:?}")"
schema_marker="$(resolve_runfile "${MINDCLADE_KUBERNETES_SCHEMA_MARKER_RLOCATION:?}")"
custom_crd_marker="$(resolve_runfile "${MINDCLADE_CUSTOM_CRD_MARKER_RLOCATION:?}")"
toolchain_manifest="$(resolve_runfile "${MINDCLADE_TOOLCHAIN_MANIFEST_RLOCATION:?}")"

for required_file in \
  "${archive}" "${extractor}" "${tool_marker}" "${schema_marker}" \
  "${custom_crd_marker}" "${toolchain_manifest}"; do
  [[ -e "${required_file}" ]] || fail "declared runfile is missing: ${required_file}"
done

staged_repository="${TEST_TMPDIR:?}/repository"
"${extractor}" "${archive}" "${staged_repository}"

# Only declared validation CLIs and the minimal POSIX userland remain visible. The original
# client PATH is neither inherited by the rule nor forwarded by this launcher.
PATH="$(dirname "${tool_marker}"):/usr/bin:/bin"
MINDCLADE_KUBERNETES_SCHEMA_DIR="$(dirname "${schema_marker}")"
MINDCLADE_CUSTOM_CRD_SCHEMA_DIR="$(dirname "${custom_crd_marker}")"
export PATH MINDCLADE_KUBERNETES_SCHEMA_DIR MINDCLADE_CUSTOM_CRD_SCHEMA_DIR
export MINDCLADE_TOOLCHAIN_MANIFEST="${toolchain_manifest}"
export MINDCLADE_VALIDATION_INTERNAL=1

unset TEST_SRCDIR TEST_WORKSPACE RUNFILES_DIR RUNFILES_MANIFEST_FILE
exec bash "${staged_repository}/infra/kubernetes/tests/validate.sh"
