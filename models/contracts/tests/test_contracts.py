# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
from pathlib import Path

import pytest

from models.contracts.target_card import (
    MODEL_TARGET_CARD_V1,
    MODEL_TARGET_CARD_V2,
    ActivationState,
    MetricDirection,
    MetricGate,
    ModelFamily,
    ModelTargetCard,
)

DATASET = "sha256:" + "d" * 64
ROOT = Path(__file__).resolve().parents[3]


def metric(**changes: object) -> MetricGate:
    values: dict[str, object] = {
        "name": "release.quality",
        "direction": MetricDirection.AT_LEAST,
        "threshold": 0.95,
        "minimum_samples": 1000,
        "evaluation_dataset_digest": DATASET,
        "required_slices": ("safety-critical",),
    }
    values.update(changes)
    return MetricGate(**values)  # type: ignore[arg-type]


def card(**changes: object) -> ModelTargetCard:
    values: dict[str, object] = {
        "model_name": "frontier-candidate",
        "family": ModelFamily.LLM,
        "owner": "modeling",
        "input_schema_digest": "sha256:" + "a" * 64,
        "output_schema_digest": "sha256:" + "b" * 64,
        "training_dataset_digests": ("sha256:" + "c" * 64,),
        "evaluation_dataset_digests": (DATASET,),
        "metric_gates": (metric(),),
        "availability_profile": "critical-serving",
        "training_hardware_profiles": ("h100-8x", "h200-8x"),
        "serving_hardware_profiles": ("cpu", "h100-8x"),
    }
    values.update(changes)
    return ModelTargetCard(**values)  # type: ignore[arg-type]


@pytest.mark.parametrize("family", list(ModelFamily))
def test_every_declared_family_uses_the_same_fail_closed_contract(family: ModelFamily) -> None:
    target = card(family=family)
    assert target.activation_state is ActivationState.DESIGNED
    assert target.qualification_evidence_digest is None


def test_gate_must_bind_a_declared_evaluation_dataset() -> None:
    with pytest.raises(ValueError, match="declared evaluation dataset"):
        card(metric_gates=(metric(evaluation_dataset_digest="sha256:" + "e" * 64),))


def test_approval_requires_immutable_qualification_evidence() -> None:
    with pytest.raises(ValueError, match="require immutable"):
        card(activation_state=ActivationState.APPROVED)
    approved = card(
        activation_state=ActivationState.APPROVED,
        qualification_evidence_digest="sha256:" + "f" * 64,
    )
    assert approved.activation_state is ActivationState.APPROVED


def test_unapproved_card_cannot_claim_evidence() -> None:
    with pytest.raises(ValueError, match="may not claim"):
        card(qualification_evidence_digest="sha256:" + "f" * 64)


def test_string_activation_state_cannot_bypass_approval_evidence() -> None:
    with pytest.raises(ValueError, match="ActivationState"):
        card(activation_state="approved")  # type: ignore[arg-type]


def test_approved_card_cannot_disable_safety_review() -> None:
    with pytest.raises(ValueError, match="safety review"):
        card(
            activation_state=ActivationState.APPROVED,
            qualification_evidence_digest="sha256:" + "f" * 64,
            safety_review_required=False,
        )


def test_metric_thresholds_and_sample_counts_are_bounded() -> None:
    with pytest.raises(ValueError, match="finite"):
        metric(threshold=float("nan"))
    with pytest.raises(ValueError, match="bounded"):
        metric(minimum_samples=0)


def test_v2_document_round_trip_is_exact() -> None:
    target = card()
    assert target.schema_version == MODEL_TARGET_CARD_V2
    assert ModelTargetCard.from_document(target.to_document()) == target
    assert target.to_document()["safetyReviewRequired"] is True


def test_v1_document_remains_a_read_only_compatibility_seam() -> None:
    legacy = card(
        schema_version=MODEL_TARGET_CARD_V1,
        metric_gates=(metric(required_slices=()),),
    )
    document = legacy.to_document()
    assert "safetyReviewRequired" not in document
    assert "requiredSlices" not in document["metricGates"][0]  # type: ignore[index]
    assert ModelTargetCard.from_document(document) == legacy


def test_v2_requires_predeclared_evaluation_slices() -> None:
    with pytest.raises(ValueError, match="predeclare"):
        card(metric_gates=(metric(required_slices=()),))


def test_document_reader_rejects_unknown_fields_and_semantic_drift() -> None:
    document = card().to_document()
    document["mutableAlias"] = "latest"
    with pytest.raises(ValueError, match="unknown or missing"):
        ModelTargetCard.from_document(document)

    mismatched = card().to_document()
    mismatched["metricGates"][0]["evaluationDatasetDigest"] = "sha256:" + "e" * 64  # type: ignore[index]
    with pytest.raises(ValueError, match="declared evaluation dataset"):
        ModelTargetCard.from_document(mismatched)


@pytest.mark.parametrize(
    ("fixture", "version"),
    [
        ("model-target-card-v1.valid.json", MODEL_TARGET_CARD_V1),
        ("model-target-card.valid.json", MODEL_TARGET_CARD_V2),
    ],
)
def test_json_fixture_and_python_contract_have_exact_parity(fixture: str, version: str) -> None:
    document = json.loads((ROOT / "configs/fixtures" / fixture).read_text(encoding="utf-8"))
    target = ModelTargetCard.from_document(document)
    assert target.schema_version == version
    assert target.to_document() == document
