# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Attestation and catalog integrity tests for infrastructure security controls."""

from copy import deepcopy
from pathlib import Path

from configs.contract_validation import load_json, validate
from infra.security.security_contracts import load_json_yaml, validate_catalog

ROOT = Path(__file__).resolve().parents[3]
SECURITY = ROOT / "infra/security"


def test_complete_security_catalog_is_fail_closed() -> None:
    assert validate_catalog(ROOT) == []


def test_node_and_image_controls_require_connected_attestation_evidence() -> None:
    node = load_json_yaml(SECURITY / "node-attestation.yaml")
    image = load_json_yaml(SECURITY / "image-policy.yaml")

    for control in (node, image):
        assert control["failurePolicy"]["mode"] == "fail-closed"
        assert control["failurePolicy"]["onMissingEvidence"] == "deny-activation"
        assert control["activationPolicy"]["enabledByDefault"] is False
        assert {"connected-gke", "signed-attestation"}.issubset(control["requiredEvidence"])


def test_schema_rejects_default_activation_and_mutable_evidence() -> None:
    schema = load_json(SECURITY / "control-contract.schema.json")
    contract = load_json_yaml(SECURITY / "node-attestation.yaml")
    assert validate(contract, schema) == ()

    invalid = deepcopy(contract)
    invalid["activationPolicy"]["enabledByDefault"] = True
    invalid["activationPolicy"]["qualificationDigestRequired"] = False
    errors = validate(invalid, schema)
    assert {error.path for error in errors}.issuperset(
        {
            "$.activationPolicy.enabledByDefault",
            "$.activationPolicy.qualificationDigestRequired",
        }
    )
