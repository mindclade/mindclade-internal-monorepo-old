# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model-weight access boundary tests for the infrastructure security catalog."""

from pathlib import Path

from infra.security.security_contracts import load_json_yaml

ROOT = Path(__file__).resolve().parents[1]


def test_weight_access_requires_key_identity_and_audit_evidence() -> None:
    contract = load_json_yaml(ROOT / "model-weight-access.yaml")

    assert contract["enforcementSources"] == [
        "infra/terraform/modules/kms/main.tf",
        "infra/terraform/modules/object_storage/main.tf",
        "infra/terraform/modules/workload_identity/main.tf",
    ]
    assert contract["requiredEvidence"] == [
        "access-review",
        "audit-query",
        "synthetic-allow-deny",
        "workload-identity-proof",
    ]


def test_weight_access_never_retries_a_denied_decision() -> None:
    contract = load_json_yaml(ROOT / "model-weight-access.yaml")

    assert contract["failurePolicy"] == {
        "mode": "fail-closed",
        "onMissingEvidence": "deny-activation",
        "retry": {"strategy": "none", "maxAttempts": 0},
    }
    assert contract["rollbackPolicy"]["strategy"] == "manual-containment"
    assert all("model weights" not in item.lower() for item in contract["enforcementSources"])
