#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

policy_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
profile="$policy_dir/testdata/synthetic-profile.json"
check_plan="$policy_dir/check-plan.sh"
test_log_dir=$(mktemp -d "${TMPDIR:-/tmp}/mindclade-terraform-policy-test.XXXXXX")
trap 'rm -rf -- "$test_log_dir"' EXIT

expect_pass() {
  local name=$1
  shift
  if ! "$@" >"$test_log_dir/$name.log" 2>&1; then
    printf 'Expected pass: %s\n' "$name" >&2
    sed -n '1,200p' "$test_log_dir/$name.log" >&2
    return 1
  fi
}

expect_fail() {
  local name=$1
  shift
  if "$@" >"$test_log_dir/$name.log" 2>&1; then
    printf 'Expected failure: %s\n' "$name" >&2
    sed -n '1,200p' "$test_log_dir/$name.log" >&2
    return 1
  fi
}

materialize_current_approval() {
  local source_path=$1
  local output_path=$2
  local window=${3:-bounded}
  python3 - "$source_path" "$output_path" "$window" <<'PY'
import json
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

source = Path(sys.argv[1])
output = Path(sys.argv[2])
window = sys.argv[3]
document = json.loads(source.read_text(encoding="utf-8"))
now = datetime.now(timezone.utc).replace(microsecond=0)
issued_at = now - timedelta(minutes=1)
expires_at = now + timedelta(hours=1)
if window == "long":
    issued_at = now - timedelta(hours=24)
    expires_at = now + timedelta(hours=1)
for approval in document["approvals"]:
    approval["issued_at"] = issued_at.isoformat().replace("+00:00", "Z")
    approval["expires_at"] = expires_at.isoformat().replace("+00:00", "Z")
output.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")
PY
}

materialize_current_approval \
  "$policy_dir/testdata/admin-approval.json" \
  "$test_log_dir/admin-approval.json"
materialize_current_approval \
  "$policy_dir/testdata/admin-approval-wildcard.json" \
  "$test_log_dir/admin-approval-wildcard.json"
materialize_current_approval \
  "$policy_dir/testdata/admin-approval-wrong-digest.json" \
  "$test_log_dir/admin-approval-wrong-digest.json"
materialize_current_approval \
  "$policy_dir/testdata/admin-approval-long-lived.json" \
  "$test_log_dir/admin-approval-long-lived.json" \
  long
materialize_current_approval \
  "$policy_dir/testdata/destructive-approval.json" \
  "$test_log_dir/destructive-approval.json"
materialize_current_approval \
  "$policy_dir/testdata/admin-approval-unknown-target.json" \
  "$test_log_dir/admin-approval-unknown-target.json"

expect_pass unit conftest verify --policy "$policy_dir"
expect_pass valid "$check_plan" \
  --plan "$policy_dir/testdata/valid-plan.json" \
  --profile "$profile"
expect_pass iam-known "$check_plan" \
  --plan "$policy_dir/testdata/iam-known-plan.json" \
  --profile "$profile"
expect_fail iam-authoritative "$check_plan" \
  --plan "$policy_dir/testdata/iam-authoritative-plan.json" \
  --profile "$profile"
grep -F 'AUTHORITATIVE_IAM_FORBIDDEN' "$test_log_dir/iam-authoritative.log" >/dev/null
for address in \
  google_project_iam_binding.forbidden \
  google_project_iam_policy.forbidden; do
  grep -F "$address" "$test_log_dir/iam-authoritative.log" >/dev/null
done
expect_fail iam-unresolved "$check_plan" \
  --plan "$policy_dir/testdata/iam-unresolved-plan.json" \
  --profile "$profile"
for code in IAM_GRANT_UNRESOLVED IAM_POLICY_UNPARSEABLE; do
  grep -F "$code" "$test_log_dir/iam-unresolved.log" >/dev/null
done
for address in \
  google_project_iam_member.unknown_member \
  google_project_iam_member.unknown_role \
  google_project_iam_binding.malformed_members \
  google_project_iam_binding.partial_unknown_members \
  google_project_iam_policy.unknown_policy_data \
  google_project_iam_policy.malformed_binding; do
  grep -F "$address" "$test_log_dir/iam-unresolved.log" >/dev/null
done
expect_fail invalid "$check_plan" \
  --plan "$policy_dir/testdata/invalid-plan.json" \
  --profile "$profile"
for code in \
  IAM_PUBLIC_PRINCIPAL \
  IAM_PRIMITIVE_ROLE \
  IAM_ADMIN_APPROVAL_REQUIRED \
  SERVICE_ACCOUNT_KEY_FORBIDDEN \
  REQUIRED_LABEL_MISSING \
  DESTRUCTIVE_APPROVAL_REQUIRED; do
  grep -F "$code" "$test_log_dir/invalid.log" >/dev/null
done
expect_fail governance-invalid "$check_plan" \
  --plan "$policy_dir/testdata/governance-invalid-plan.json" \
  --profile "$profile"
for code in \
  CMEK_REQUIRED \
  RETENTION_INSUFFICIENT \
  LOCATION_NOT_APPROVED \
  RESIDENCY_VIOLATION \
  DELETION_PROTECTION_REQUIRED; do
  grep -F "$code" "$test_log_dir/governance-invalid.log" >/dev/null
done
expect_fail admin-unapproved "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile"
expect_pass admin-approved "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/admin-approval.json"
expect_fail admin-expired "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile" \
  --approval "$policy_dir/testdata/admin-approval-expired.json"
expect_fail admin-wildcard "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/admin-approval-wildcard.json"
expect_fail admin-wrong-digest "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/admin-approval-wrong-digest.json"
expect_fail admin-long-lived "$check_plan" \
  --plan "$policy_dir/testdata/admin-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/admin-approval-long-lived.json"
grep -F 'APPROVAL_INVALID' "$test_log_dir/admin-long-lived.log" >/dev/null
expect_fail admin-unknown-target "$check_plan" \
  --plan "$policy_dir/testdata/iam-unknown-target-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/admin-approval-unknown-target.json"
grep -F 'IAM_TARGET_UNRESOLVED' "$test_log_dir/admin-unknown-target.log" >/dev/null
expect_fail destructive-unapproved "$check_plan" \
  --plan "$policy_dir/testdata/destructive-plan.json" \
  --profile "$profile"
expect_pass destructive-approved "$check_plan" \
  --plan "$policy_dir/testdata/destructive-plan.json" \
  --profile "$profile" \
  --approval "$test_log_dir/destructive-approval.json"
expect_fail empty-profile "$check_plan" \
  --plan "$policy_dir/testdata/valid-plan.json" \
  --profile "$policy_dir/testdata/empty-profile.json"
expect_fail fractional-profile "$check_plan" \
  --plan "$policy_dir/testdata/valid-plan.json" \
  --profile "$policy_dir/testdata/fractional-profile.json"
grep -F 'POLICY_CONTEXT_INVALID' "$test_log_dir/fractional-profile.log" >/dev/null
expect_fail missing-plan "$check_plan" --profile "$profile"
expect_fail missing-profile "$check_plan" \
  --plan "$policy_dir/testdata/valid-plan.json"

printf 'Terraform plan policy tests passed.\n'
