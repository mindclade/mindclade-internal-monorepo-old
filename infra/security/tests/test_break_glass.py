# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Emergency-access lifecycle tests for the infrastructure security catalog."""

from pathlib import Path

from infra.security.security_contracts import load_json_yaml

ROOT = Path(__file__).resolve().parents[1]


def test_break_glass_requires_manual_time_bounded_revocation() -> None:
    contract = load_json_yaml(ROOT / "break-glass.yaml")

    assert contract["requiredEvidence"] == [
        "access-review",
        "audit-query",
        "revocation-proof",
        "time-bound-approval",
    ]
    assert contract["failurePolicy"]["retry"] == {
        "strategy": "manual",
        "maxAttempts": 0,
    }
    assert contract["rollbackPolicy"] == {
        "strategy": "manual-containment",
        "preserveAuditTrail": True,
        "requiresOwner": True,
    }


def test_break_glass_sources_include_access_identity_and_plan_policy() -> None:
    contract = load_json_yaml(ROOT / "break-glass.yaml")
    assert contract["enforcementSources"] == [
        "infra/terraform/modules/iap_access/main.tf",
        "infra/terraform/modules/workload_identity/main.tf",
        "infra/terraform/policy/terraform_plan.rego",
    ]
    assert contract["activationPolicy"]["environmentOwned"] is True
    assert contract["activationPolicy"]["exactRevisionRequired"] is True
