#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

set -euo pipefail
IFS=$'\n\t'

runner="${TEST_SRCDIR:?}/${MINDCLADE_TERRAFORM_TEST_RUNNER_RLOCATION:?}"
# shellcheck source=/dev/null
source "$runner"

fixture="${TEST_TMPDIR:?}/runfiles-manifest"
resolved="${TEST_TMPDIR}/resolved path"
touch "$resolved"
printf 'repo+extension+canonical/path %s\n' "$resolved" >"$fixture"

export RUNFILES_DIR=""
export RUNFILES_MANIFEST_FILE="$fixture"
actual="$(resolve_runfile 'repo+extension+canonical/path')"
[[ "$actual" == "$resolved" ]] || {
  printf 'ERROR: literal manifest lookup returned %q, expected %q\n' "$actual" "$resolved" >&2
  exit 1
}
