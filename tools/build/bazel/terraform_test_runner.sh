#!/usr/bin/env bash
set -euo pipefail

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
  printf 'unable to resolve runfile: %s\n' "$logical" >&2
  return 1
}

tool_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_TOOL_MARKER:?}")"
provider_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_PROVIDER_MIRROR_MARKER:?}")"
module_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_MODULE_MARKER:?}")"

export PATH="$(dirname "$tool_marker"):${PATH}"
export HOME="${TEST_TMPDIR:?}/home"
export TF_DATA_DIR="${TEST_TMPDIR}/terraform-data"
export TF_CLI_CONFIG_FILE="${TEST_TMPDIR}/terraform.rc"
export TF_IN_AUTOMATION=1
export CHECKPOINT_DISABLE=1
mkdir -p "$HOME" "$TF_DATA_DIR"

provider_mirror="$(dirname "$provider_marker")"
cat >"$TF_CLI_CONFIG_FILE" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$provider_mirror"
    include = ["registry.terraform.io/hashicorp/google"]
  }
}
EOF

module_dir="$(dirname "$module_marker")"
terraform -chdir="$module_dir" init \
  -backend=false \
  -input=false \
  -lockfile=readonly \
  -no-color
terraform -chdir="$module_dir" test -no-color
