#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

usage() {
  printf '%s\n' \
    'Usage: check-plan.sh --plan <plan.json> --profile <profile.json> [--approval <approval.json>]' \
    '' \
    'Evaluates Terraform plan JSON with the repository Conftest policy.' \
    'The plan digest is computed from the exact input bytes and injected as runtime data.'
}

fail() {
  printf 'check-plan: %s\n' "$1" >&2
  exit 2
}

plan_path=''
profile_path=''
approval_path=''

while (($# > 0)); do
  case "$1" in
    --plan)
      (($# >= 2)) || fail '--plan requires a path'
      plan_path=$2
      shift 2
      ;;
    --profile)
      (($# >= 2)) || fail '--profile requires a path'
      profile_path=$2
      shift 2
      ;;
    --approval)
      (($# >= 2)) || fail '--approval requires a path'
      approval_path=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$plan_path" ]] || fail '--plan is required'
[[ -n "$profile_path" ]] || fail '--profile is required'
[[ -s "$plan_path" ]] || fail "plan does not exist or is empty: $plan_path"
[[ -s "$profile_path" ]] || fail "profile does not exist or is empty: $profile_path"
[[ -z "$approval_path" || -s "$approval_path" ]] || fail "approval does not exist or is empty: $approval_path"

command -v conftest >/dev/null 2>&1 || fail 'conftest is required'
command -v python3 >/dev/null 2>&1 || fail 'python3 is required'

policy_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
runtime_dir=$(mktemp -d "${TMPDIR:-/tmp}/mindclade-terraform-policy.XXXXXX")
trap 'rm -rf -- "$runtime_dir"' EXIT

prepare_args=(
  --plan "$plan_path"
  --profile "$profile_path"
  --output "$runtime_dir/context.json"
)
if [[ -n "$approval_path" ]]; then
  prepare_args+=(--approval "$approval_path")
fi

# This preflight is deliberately redundant with Rego. It prevents a missing
# architecture decision from looking like a clean policy evaluation and emits a
# direct error before Conftest starts.
python3 "$policy_dir/prepare_context.py" "${prepare_args[@]}"

conftest test \
  --policy "$policy_dir" \
  --data "$runtime_dir/context.json" \
  "$plan_path"
