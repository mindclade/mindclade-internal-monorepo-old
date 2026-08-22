#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
IFS=$'\n\t'

resolve_runfile() {
  local logical="$1"
  local resolved
  if [[ "$logical" = /* && -e "$logical" ]]; then
    printf '%s\n' "$logical"
    return 0
  fi
  if [[ -e "$logical" ]]; then
    printf '%s\n' "$logical"
    return 0
  fi
  if [[ -n "${RUNFILES_DIR:-}" && -e "${RUNFILES_DIR}/${logical}" ]]; then
    printf '%s\n' "${RUNFILES_DIR}/${logical}"
    return 0
  fi
  if [[ -n "${TEST_SRCDIR:-}" && -e "${TEST_SRCDIR}/${logical}" ]]; then
    printf '%s\n' "${TEST_SRCDIR}/${logical}"
    return 0
  fi
  if [[ -n "${RUNFILES_MANIFEST_FILE:-}" && -f "${RUNFILES_MANIFEST_FILE}" ]]; then
    resolved="$(awk -v key="$logical" '$1 == key {sub($1 FS, ""); print; exit}' "${RUNFILES_MANIFEST_FILE}")"
    if [[ -n "$resolved" && -e "$resolved" ]]; then
      printf '%s\n' "$resolved"
      return 0
    fi
  fi
  printf 'ERROR: unable to resolve runfile: %s\n' "$logical" >&2
  return 1
}

terraform_binary="$(resolve_runfile "${MINDCLADE_TERRAFORM_RLOCATION:?}")"
provider_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_PROVIDER_MIRROR_MARKER_RLOCATION:?}")"
module_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_MODULE_MARKER_RLOCATION:?}")"
[[ -x "$terraform_binary" ]] || {
  printf 'ERROR: Terraform executable is missing: %s\n' "$terraform_binary" >&2
  exit 1
}

task_terraform_data="${TEST_TMPDIR:?}/terraform-data"
task_terraform_config="${TEST_TMPDIR}/terraform.rc"
export TF_DATA_DIR="$task_terraform_data"
export TF_CLI_CONFIG_FILE="$task_terraform_config"
export TF_IN_AUTOMATION=1
export CHECKPOINT_DISABLE=1
mkdir -p "$task_terraform_data"

provider_mirror="$(dirname "$provider_marker")"
cat >"$task_terraform_config" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$provider_mirror"
    include = ["registry.terraform.io/hashicorp/google"]
  }
}
EOF

module_dir="$(dirname "$module_marker")"
"$terraform_binary" -chdir="$module_dir" init \
  -backend=false \
  -input=false \
  -lockfile=readonly \
  -no-color
exec "$terraform_binary" -chdir="$module_dir" test -no-color
