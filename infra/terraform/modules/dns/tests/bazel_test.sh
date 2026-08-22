#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
IFS=$'\n\t'

resolve_runfile() {
  local relative_path="$1"
  printf '%s/%s\n' "${TEST_SRCDIR:?}" "${relative_path}"
}

terraform_binary="$(resolve_runfile "${MINDCLADE_TERRAFORM_RLOCATION:?}")"
[[ -x "${terraform_binary}" ]] || {
  printf 'ERROR: Nix-owned Terraform executable is missing: %s\n' "${terraform_binary}" >&2
  exit 1
}
PATH="/usr/bin:/bin"
export PATH

module_dir="${TEST_SRCDIR:?}/${TEST_WORKSPACE:?}/infra/terraform/modules/dns"
[[ -d "${module_dir}" ]] || {
  printf 'ERROR: DNS module runfiles are missing: %s\n' "${module_dir}" >&2
  exit 1
}

cd "${module_dir}"
TF_DATA_DIR="${TEST_TMPDIR:?}/terraform-data"
export TF_DATA_DIR
mkdir -p "${TF_DATA_DIR}/modules"
printf '%s\n' '{"Modules":[{"Key":"","Source":"","Dir":"."},{"Key":"test.tests.dns.retired_dns_hub_is_only_a_composition_fixture","Source":"./tests/fixtures/dns_hub","Dir":"tests/fixtures/dns_hub"},{"Key":"test.tests.dns.retired_dns_hub_is_only_a_composition_fixture.public_zones","Source":"../../..","Dir":"."}]}' >"${TF_DATA_DIR}/modules/modules.json"

provider_marker="$(resolve_runfile "${MINDCLADE_TERRAFORM_GOOGLE_PROVIDER_MARKER_RLOCATION:?}")"
[[ -f "${provider_marker}" ]] || {
  printf 'ERROR: Terraform Google provider marker is missing: %s\n' "${provider_marker}" >&2
  exit 1
}
provider_platform="$(cat "${provider_marker}")"
provider_destination="${TF_DATA_DIR}/providers/registry.terraform.io/hashicorp/google/7.45.0/${provider_platform}"
mkdir -p "${provider_destination}"
for provider_file in "$(dirname "${provider_marker}")"/*; do
  cp -R "${provider_file}" "${provider_destination}/"
done

exec "${terraform_binary}" test
