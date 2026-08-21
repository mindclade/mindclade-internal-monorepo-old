# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
from pathlib import Path

import pytest

from configs.contract_validation import load_json
from tools.qualification.evidence import evidence_digest, to_gitops_record, validate_evidence

ROOT = Path(__file__).resolve().parents[1]


def fixture() -> dict:
    return load_json(ROOT / "fixtures/release-evidence.valid.json")


def schema() -> dict:
    return load_json(ROOT / "schemas/release-evidence.schema.json")


def test_valid_fixture_has_stable_content_digest() -> None:
    value = fixture()
    assert validate_evidence(value, schema()) == ()
    assert evidence_digest(value) == evidence_digest(copy.deepcopy(value))


@pytest.mark.parametrize("severity", ["critical", "high", "unknown"])
def test_passing_scan_rejects_unresolved_findings(severity: str) -> None:
    value = fixture()
    value["vulnerability"]["finding_counts"][severity] = 1
    assert any(
        "zero critical, high, and unknown" in error for error in validate_evidence(value, schema())
    )


def test_exception_requires_security_approval_and_bounded_expiry() -> None:
    value = fixture()
    value["vulnerability"]["result"] = "approved-exception"
    value["vulnerability"]["finding_counts"]["high"] = 1
    value["vulnerability"]["exception"] = {
        "ticket": "SEC-123",
        "approved_by": "@mindclade/platform",
        "approved_at": "2026-08-20T00:00:00Z",
        "expires_at": "2027-08-20T00:00:00Z",
        "justification": "fixture",
    }
    errors = validate_evidence(value, schema())
    assert any("@mindclade/security" in error for error in errors)
    assert any("within 90 days" in error for error in errors)


def test_bounded_security_exception_is_explicitly_bound_into_graph() -> None:
    value = fixture()
    value["vulnerability"]["result"] = "approved-exception"
    value["vulnerability"]["finding_counts"]["high"] = 1
    value["vulnerability"]["exception"] = {
        "ticket": "SEC-123",
        "approved_by": "@mindclade/security",
        "approved_at": "2026-08-20T00:02:00Z",
        "expires_at": "2026-08-21T00:02:00Z",
        "justification": "Time-bounded fixture exception with compensating isolation.",
    }
    value["evidence"]["graph"][3]["result"] = "approved"
    assert validate_evidence(value, schema()) == ()


def test_vulnerability_graph_must_match_scan_decision() -> None:
    value = fixture()
    value["evidence"]["graph"][3]["result"] = "approved"
    assert any("graph result" in error for error in validate_evidence(value, schema()))


def test_evidence_graph_binds_subject_and_typed_artifact() -> None:
    value = fixture()
    value["evidence"]["graph"][0]["subject_digest"] = "sha256:" + "9" * 64
    value["evidence"]["graph"][1]["artifact"] = "sbom"
    errors = validate_evidence(value, schema())
    assert any("does not bind" in error for error in errors)
    assert any("wrong artifact" in error for error in errors)


def test_gitops_handoff_adds_only_governed_deployment_root() -> None:
    value = fixture()
    record = to_gitops_record(
        value,
        deployment_project="mc-production-security",
        deployment_attestor="deployment-attestor",
        signer_workflow_ref="mindclade/.github/.github/workflows/reusable-binauthz-sign.yml@refs/tags/v4.0.0",
    )
    assert "schema_version" not in record
    assert record["contract_version"] == "4.0.0"
    assert record["attestations"]["deployment"]["attestor"] == "deployment-attestor"
    assert "deployment" not in value["attestations"]


def test_mutable_signer_workflow_is_rejected() -> None:
    with pytest.raises(ValueError, match="immutable"):
        to_gitops_record(
            fixture(),
            deployment_project="mc-production-security",
            deployment_attestor="deployment-attestor",
            signer_workflow_ref="mindclade/.github/.github/workflows/reusable-binauthz-sign.yml@main",
        )


def test_invalid_evidence_cannot_enter_gitops_handoff() -> None:
    value = fixture()
    value["vulnerability"]["finding_counts"]["critical"] = 1
    with pytest.raises(ValueError, match="invalid producer evidence"):
        to_gitops_record(
            value,
            deployment_project="mc-production-security",
            deployment_attestor="deployment-attestor",
            signer_workflow_ref="mindclade/.github/.github/workflows/reusable-binauthz-sign.yml@refs/tags/v4.0.0",
        )


def test_deployment_attestor_must_be_independent() -> None:
    with pytest.raises(ValueError, match="distinct"):
        to_gitops_record(
            fixture(),
            deployment_project="mc-build-security",
            deployment_attestor="build-attestor",
            signer_workflow_ref="mindclade/.github/.github/workflows/reusable-binauthz-sign.yml@refs/tags/v4.0.0",
        )
