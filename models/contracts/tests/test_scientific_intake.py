# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
import json
from dataclasses import replace
from pathlib import Path

import pytest

from libs.python.identifiers import ArtifactRef, Digest
from models.contracts.scientific_intake import (
    ApprovalAttestation,
    ApprovalRole,
    IntakePurpose,
    ScientificModelIntake,
)

ROOT = Path(__file__).resolve().parents[3]


def ref(name: str, *, kind: str, media_type: str, schema_version: int = 1) -> ArtifactRef:
    return ArtifactRef(
        digest=Digest.of(name.encode()),
        size_bytes=128,
        media_type=media_type,
        logical_kind=kind,
        schema_version=schema_version,
    )


def intake() -> ScientificModelIntake:
    target = ref(
        "target",
        kind="model-target-card",
        media_type="application/vnd.mindclade.model-target-card+json",
        schema_version=2,
    )
    approvals = tuple(
        ApprovalAttestation(
            role=role,
            subject_digest=target.digest,
            attestation=ref(
                f"approval-{role.value}",
                kind="scientific-intake-attestation",
                media_type="application/vnd.mindclade.scientific-intake-attestation+json",
            ),
        )
        for role in ApprovalRole
        if role is not ApprovalRole.BIOSECURITY
    )
    return ScientificModelIntake(
        intake_id="candidate-intake",
        purpose=IntakePurpose.IMPLEMENTATION_AUTHORIZATION,
        target_card=target,
        scientific_semantics=ref(
            "semantics",
            kind="scientific-semantics",
            media_type="application/vnd.mindclade.scientific-semantics+json",
        ),
        preprocessing_contract=ref(
            "preprocessing",
            kind="preprocessing-contract",
            media_type="application/vnd.mindclade.preprocessing-contract+json",
        ),
        checkpoint_contract=ref(
            "checkpoint",
            kind="checkpoint-contract",
            media_type="application/vnd.mindclade.checkpoint-contract+json",
        ),
        reference_vector_pack=ref(
            "vectors",
            kind="reference-vector-pack",
            media_type="application/vnd.mindclade.reference-vector-pack+tar",
        ),
        training_datasets=(
            ref(
                "training",
                kind="dataset-manifest",
                media_type="application/vnd.mindclade.dataset-manifest+json",
            ),
        ),
        evaluation_datasets=(
            ref(
                "evaluation",
                kind="dataset-manifest",
                media_type="application/vnd.mindclade.dataset-manifest+json",
            ),
        ),
        evaluation_policy=ref(
            "evaluation-policy",
            kind="evaluation-policy",
            media_type="application/vnd.mindclade.evaluation-policy+json",
        ),
        serving_contract=ref(
            "serving",
            kind="serving-contract",
            media_type="application/vnd.mindclade.serving-contract+json",
        ),
        runtime_consumers=(
            ref(
                "runtime",
                kind="serving-runtime-contract",
                media_type="application/vnd.mindclade.serving-runtime-contract+json",
            ),
        ),
        safety_use_policy=ref(
            "safety",
            kind="safety-use-policy",
            media_type="application/vnd.mindclade.safety-use-policy+json",
        ),
        source_revision="1" * 40,
        policy_digest=Digest.of(b"policy"),
        biosecurity_review_required=False,
        approvals=approvals,
    )


def test_document_round_trip_and_digest_are_deterministic() -> None:
    candidate = intake()
    restored = ScientificModelIntake.from_document(candidate.to_document())
    assert restored == candidate
    assert restored.canonical_bytes() == candidate.canonical_bytes()
    assert restored.digest == candidate.digest


def test_input_order_does_not_change_intake_identity() -> None:
    candidate = intake()
    reversed_approvals = tuple(reversed(candidate.approvals))
    reordered = replace(candidate, approvals=reversed_approvals)
    assert reordered.digest == candidate.digest


def test_mutable_alias_and_unknown_fields_are_rejected() -> None:
    document = copy.deepcopy(intake().to_document())
    document["targetCard"]["uri"] = "gs://mutable/latest"  # type: ignore[index]
    with pytest.raises(ValueError, match="canonical ArtifactRef"):
        ScientificModelIntake.from_document(document)

    document = intake().to_document()
    document["registryMutation"] = True
    with pytest.raises(ValueError, match="unknown or missing"):
        ScientificModelIntake.from_document(document)


def test_source_and_collection_bounds_fail_closed() -> None:
    document = intake().to_document()
    document["sourceRevision"] = "main"
    with pytest.raises(ValueError, match="full lowercase Git commit"):
        ScientificModelIntake.from_document(document)

    document = intake().to_document()
    document["trainingDatasets"] = []
    with pytest.raises(ValueError, match="bounded count"):
        ScientificModelIntake.from_document(document)


def test_json_fixture_and_python_contract_have_exact_parity() -> None:
    document = json.loads(
        (ROOT / "configs/fixtures/scientific-model-intake.valid.json").read_text(encoding="utf-8")
    )
    assert ScientificModelIntake.from_document(document).to_document() == document
