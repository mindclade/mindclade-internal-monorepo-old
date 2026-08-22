# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from dataclasses import replace

from libs.python.identifiers import ArtifactRef, Digest
from models.contracts.scientific_intake import (
    ApprovalAttestation,
    ApprovalRole,
    IntakePurpose,
    ScientificModelIntake,
)
from models.contracts.target_card import (
    MODEL_TARGET_CARD_V1,
    ActivationState,
    MetricDirection,
    MetricGate,
    ModelFamily,
    ModelTargetCard,
)
from qualification.scientific_model_intake.gate import (
    APPROVAL_CONTRACT,
    CHECKPOINT_CONTRACT,
    DATASET_CONTRACT,
    EVALUATION_POLICY_CONTRACT,
    PREPROCESSING_CONTRACT,
    REFERENCE_VECTOR_CONTRACT,
    RUNTIME_CONSUMER_CONTRACT,
    SAFETY_USE_CONTRACT,
    SCIENTIFIC_SEMANTICS_CONTRACT,
    SERVING_CONTRACT,
    TARGET_CARD_CONTRACT,
    ArtifactContract,
    RejectionReason,
    ResolvedArtifact,
    evaluate_scientific_intake,
)


def artifact(name: str, contract: ArtifactContract) -> ArtifactRef:
    return ArtifactRef(
        digest=Digest.of(name.encode()),
        size_bytes=128,
        media_type=contract.media_type,
        logical_kind=contract.logical_kind,
        schema_version=contract.schema_version,
    )


def target_card(
    training: ArtifactRef, evaluation: ArtifactRef, **changes: object
) -> ModelTargetCard:
    values: dict[str, object] = {
        "model_name": "scientific-candidate",
        "family": ModelFamily.BIOLOGY,
        "owner": "modeling",
        "input_schema_digest": Digest.of(b"input-schema").text,
        "output_schema_digest": Digest.of(b"output-schema").text,
        "training_dataset_digests": (training.digest.text,),
        "evaluation_dataset_digests": (evaluation.digest.text,),
        "metric_gates": (
            MetricGate(
                name="release.quality",
                direction=MetricDirection.AT_LEAST,
                threshold=0.9,
                minimum_samples=100,
                evaluation_dataset_digest=evaluation.digest.text,
                required_slices=("safety-critical",),
            ),
        ),
        "availability_profile": "critical-serving",
        "training_hardware_profiles": ("h100-8x",),
        "serving_hardware_profiles": ("h100-8x",),
    }
    values.update(changes)
    return ModelTargetCard(**values)  # type: ignore[arg-type]


def candidate() -> tuple[ScientificModelIntake, ModelTargetCard]:
    target_ref = artifact("target-card", TARGET_CARD_CONTRACT)
    training = artifact("training-dataset", DATASET_CONTRACT)
    evaluation = artifact("evaluation-dataset", DATASET_CONTRACT)
    card = target_card(training, evaluation)
    approvals = tuple(
        ApprovalAttestation(
            role=role,
            subject_digest=target_ref.digest,
            attestation=artifact(f"approval-{role.value}", APPROVAL_CONTRACT),
        )
        for role in ApprovalRole
        if role is not ApprovalRole.BIOSECURITY
    )
    return (
        ScientificModelIntake(
            intake_id="scientific-candidate",
            purpose=IntakePurpose.IMPLEMENTATION_AUTHORIZATION,
            target_card=target_ref,
            scientific_semantics=artifact("semantics", SCIENTIFIC_SEMANTICS_CONTRACT),
            preprocessing_contract=artifact("preprocessing", PREPROCESSING_CONTRACT),
            checkpoint_contract=artifact("checkpoint", CHECKPOINT_CONTRACT),
            reference_vector_pack=artifact("vectors", REFERENCE_VECTOR_CONTRACT),
            training_datasets=(training,),
            evaluation_datasets=(evaluation,),
            evaluation_policy=artifact("evaluation-policy", EVALUATION_POLICY_CONTRACT),
            serving_contract=artifact("serving", SERVING_CONTRACT),
            runtime_consumers=(artifact("runtime", RUNTIME_CONSUMER_CONTRACT),),
            safety_use_policy=artifact("safety", SAFETY_USE_CONTRACT),
            source_revision="1" * 40,
            policy_digest=Digest.of(b"active-policy"),
            biosecurity_review_required=False,
            approvals=approvals,
        ),
        card,
    )


class Resolver:
    def __init__(self, intake: ScientificModelIntake, card: ModelTargetCard) -> None:
        self.values = {
            reference.digest.text: ResolvedArtifact(reference)
            for _, reference in intake.artifact_bindings()
        }
        self.values[intake.target_card.digest.text] = ResolvedArtifact(
            intake.target_card, card.to_document()
        )
        self.calls: list[str] = []

    def resolve(self, digest: Digest) -> ResolvedArtifact | None:
        self.calls.append(digest.text)
        return self.values.get(digest.text)


def decide(
    intake: ScientificModelIntake,
    card: ModelTargetCard,
    *,
    resolver: Resolver | None = None,
    policy: Digest | None = None,
):
    selected_resolver = resolver or Resolver(intake, card)
    return evaluate_scientific_intake(
        intake,
        active_policy_digest=policy or intake.policy_digest,
        resolver=selected_resolver,
    )


def test_complete_intake_is_deterministically_implementation_only() -> None:
    intake, card = candidate()
    resolver = Resolver(intake, card)
    first = decide(intake, card, resolver=resolver)
    second = decide(intake, card, resolver=Resolver(intake, card))

    assert first.accepted
    assert first.rejection_reasons == ()
    assert first.authorization_scope == "implementation-only"
    assert first.canonical_bytes() == second.canonical_bytes()
    assert first.digest == second.digest
    assert len(resolver.calls) == len(intake.artifact_bindings())


def test_template_policy_vectors_and_runtime_fail_closed() -> None:
    intake, card = candidate()
    incomplete = replace(
        intake,
        purpose=IntakePurpose.TEMPLATE,
        reference_vector_pack=None,
        runtime_consumers=(),
    )
    result = decide(incomplete, card, policy=Digest.of(b"different-policy"))
    assert not result.accepted
    assert set(result.rejection_reasons) == {
        RejectionReason.MISSING_REFERENCE_VECTORS,
        RejectionReason.POLICY_DIGEST_MISMATCH,
        RejectionReason.RUNTIME_CONSUMER_REQUIRED,
        RejectionReason.TEMPLATE_INTAKE,
    }


def test_dataset_leakage_duplicate_identity_and_target_mismatch_are_rejected() -> None:
    intake, card = candidate()
    leaked = replace(intake, evaluation_datasets=intake.training_datasets)
    result = decide(leaked, card)
    assert RejectionReason.TRAIN_EVALUATION_DATA_OVERLAP in result.rejection_reasons
    assert RejectionReason.DUPLICATE_ARTIFACT_DIGEST in result.rejection_reasons
    assert RejectionReason.TARGET_CARD_DATASET_MISMATCH in result.rejection_reasons


def test_resolver_and_artifact_identity_fail_closed() -> None:
    intake, card = candidate()
    wrong_contract = replace(
        intake.serving_contract,
        logical_kind="mutable-alias",
    )
    malformed = replace(intake, serving_contract=wrong_contract)
    resolver = Resolver(malformed, card)
    resolver.values.pop(malformed.evaluation_policy.digest.text)
    resolver.values[malformed.checkpoint_contract.digest.text] = ResolvedArtifact(
        replace(malformed.checkpoint_contract, size_bytes=129)
    )
    result = decide(malformed, card, resolver=resolver)
    assert RejectionReason.ARTIFACT_CONTRACT_MISMATCH in result.rejection_reasons
    assert RejectionReason.ARTIFACT_IDENTITY_MISMATCH in result.rejection_reasons
    assert RejectionReason.UNRESOLVED_ARTIFACT in result.rejection_reasons


def test_approval_roles_subjects_and_biosecurity_are_enforced() -> None:
    intake, card = candidate()
    modeling = next(item for item in intake.approvals if item.role is ApprovalRole.MODELING)
    incomplete = replace(
        intake,
        biosecurity_review_required=True,
        approvals=(
            replace(modeling, subject_digest=Digest.of(b"wrong-subject")),
            modeling,
            *tuple(item for item in intake.approvals if item.role is not ApprovalRole.SECURITY),
        ),
    )
    result = decide(incomplete, card)
    assert RejectionReason.APPROVAL_SUBJECT_MISMATCH in result.rejection_reasons
    assert RejectionReason.DUPLICATE_APPROVAL_ROLE in result.rejection_reasons
    assert RejectionReason.DUPLICATE_ARTIFACT_DIGEST in result.rejection_reasons
    assert RejectionReason.MISSING_REQUIRED_APPROVAL in result.rejection_reasons


def test_only_designed_safe_v2_target_cards_are_eligible() -> None:
    intake, card = candidate()
    v1_card = replace(
        card,
        schema_version=MODEL_TARGET_CARD_V1,
        metric_gates=(replace(card.metric_gates[0], required_slices=()),),
    )
    assert RejectionReason.TARGET_CARD_V2_REQUIRED in decide(intake, v1_card).rejection_reasons

    qualification_card = replace(card, activation_state=ActivationState.QUALIFICATION)
    assert (
        RejectionReason.TARGET_CARD_MUST_BE_DESIGNED
        in decide(intake, qualification_card).rejection_reasons
    )

    unsafe_card = replace(card, safety_review_required=False)
    assert (
        RejectionReason.TARGET_CARD_SAFETY_REVIEW_REQUIRED
        in decide(intake, unsafe_card).rejection_reasons
    )


def test_invalid_resolved_target_card_is_not_treated_as_authorization() -> None:
    intake, card = candidate()
    resolver = Resolver(intake, card)
    resolver.values[intake.target_card.digest.text] = ResolvedArtifact(
        intake.target_card,
        {"schemaVersion": "mindclade.dev/model-target-card/v2"},
    )
    result = decide(intake, card, resolver=resolver)
    assert result.rejection_reasons == (RejectionReason.TARGET_CARD_INVALID,)
