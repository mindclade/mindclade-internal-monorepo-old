#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

set -euo pipefail
export PYTHONDONTWRITEBYTECODE=1

governance_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(CDPATH='' cd -- "${governance_dir}/../../.." && pwd)"

args=(check --repo "${repo_root}")
if [[ -n "${TERRAFORM_INTERFACE_BASE_REF:-}" ]]; then
  args+=(--base-ref "${TERRAFORM_INTERFACE_BASE_REF}")
fi

exec python3 "${governance_dir}/interface_governance.py" "${args[@]}" "$@"
