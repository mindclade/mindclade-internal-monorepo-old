# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Read-only scientific model contract-intake decision.

The gate resolves immutable identities and returns an implementation-only
authorization record. It intentionally has no registry, release, maturity, or
deployment dependency and cannot promote a model as a side effect.
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum
from typing import Final, Protocol

from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes
from models.contracts.scientific_intake import (
    ApprovalRole,
    IntakePurpose,
    ScientificModelIntake,
)
from models.contracts.target_card import (
    MODEL_TARGET_CARD_V2,
    ActivationState,
    ModelTargetCard,
)

SCIENTIFIC_INTAKE_DECISION_V1: Final = "mindclade.dev/scientific-model-intake-decision/v1"
IMPLEMENTATION_ONLY: Final = "implementation-only"


@dataclass(frozen=True, slots=True)
class ArtifactContract:
    media_type: str
    logical_kind: str
    schema_version: int


TARGET_CARD_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.model-target-card+json", "model-target-card", 2
)
SCIENTIFIC_SEMANTICS_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.scientific-semantics+json", "scientific-semantics", 1
)
PREPROCESSING_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.preprocessing-contract+json", "preprocessing-contract", 1
)
CHECKPOINT_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.checkpoint-contract+json", "checkpoint-contract", 1
)
REFERENCE_VECTOR_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.reference-vector-pack+tar", "reference-vector-pack", 1
)
DATASET_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.dataset-manifest+json", "dataset-manifest", 1
)
EVALUATION_POLICY_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.evaluation-policy+json", "evaluation-policy", 1
)
SERVING_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.serving-contract+json", "serving-contract", 1
)
RUNTIME_CONSUMER_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.serving-runtime-contract+json",
    "serving-runtime-contract",
    1,
)
SAFETY_USE_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.safety-use-policy+json", "safety-use-policy", 1
)
APPROVAL_CONTRACT: Final = ArtifactContract(
    "application/vnd.mindclade.scientific-intake-attestation+json",
    "scientific-intake-attestation",
    1,
)

_BASE_APPROVALS: Final = frozenset(
    {
        ApprovalRole.MODELING,
        ApprovalRole.DATA,
        ApprovalRole.EVALUATION_TRAINING,
        ApprovalRole.RUNTIME,
        ApprovalRole.PLATFORM_CONTROL,
        ApprovalRole.RELEASE,
        ApprovalRole.SECURITY,
    }
)


class RejectionReason(StrEnum):
    ARTIFACT_CONTRACT_MISMATCH = "artifact-contract-mismatch"
    ARTIFACT_IDENTITY_MISMATCH = "artifact-identity-mismatch"
    APPROVAL_SUBJECT_MISMATCH = "approval-subject-mismatch"
    DUPLICATE_APPROVAL_ROLE = "duplicate-approval-role"
    DUPLICATE_ARTIFACT_DIGEST = "duplicate-artifact-digest"
    MISSING_REFERENCE_VECTORS = "missing-reference-vectors"
    MISSING_REQUIRED_APPROVAL = "missing-required-approval"
    POLICY_DIGEST_MISMATCH = "policy-digest-mismatch"
    RUNTIME_CONSUMER_REQUIRED = "runtime-consumer-required"
    TARGET_CARD_DATASET_MISMATCH = "target-card-dataset-mismatch"
    TARGET_CARD_INVALID = "target-card-invalid"
    TARGET_CARD_MUST_BE_DESIGNED = "target-card-must-be-designed"
    TARGET_CARD_SAFETY_REVIEW_REQUIRED = "target-card-safety-review-required"
    TARGET_CARD_V2_REQUIRED = "target-card-v2-required"
    TEMPLATE_INTAKE = "template-intake"
    TRAIN_EVALUATION_DATA_OVERLAP = "train-evaluation-data-overlap"
    UNRESOLVED_ARTIFACT = "unresolved-artifact"


@dataclass(frozen=True, slots=True)
class ResolvedArtifact:
    """Resolver result after provider-side digest verification.

    ``document`` is required only for the target card. The resolver remains
    responsible for verifying payload bytes against ``reference.digest`` before
    decoding; the gate independently compares all five identity fields.
    """

    reference: ArtifactRef
    document: Mapping[str, object] | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.reference, ArtifactRef):
            raise ValueError("resolved artifact must carry an ArtifactRef")
        if self.document is not None and (
            not isinstance(self.document, Mapping)
            or any(not isinstance(key, str) for key in self.document)
        ):
            raise ValueError("resolved artifact document must be a string-keyed mapping")


class ScientificArtifactResolver(Protocol):
    """Read-only, authorization-aware resolver supplied by the composition root."""

    def resolve(self, digest: Digest) -> ResolvedArtifact | None: ...


@dataclass(frozen=True, slots=True)
class IntakeDecision:
    """Deterministic result; acceptance authorizes implementation and nothing else."""

    intake_digest: Digest
    policy_digest: Digest
    accepted: bool
    rejection_reasons: tuple[RejectionReason, ...]
    attestation_digests: tuple[Digest, ...]
    authorization_scope: str = IMPLEMENTATION_ONLY
    schema_version: str = SCIENTIFIC_INTAKE_DECISION_V1

    def __post_init__(self) -> None:
        if not isinstance(self.intake_digest, Digest) or not isinstance(self.policy_digest, Digest):
            raise ValueError("decision digests must be canonical")
        if not isinstance(self.accepted, bool):
            raise ValueError("accepted must be a boolean")
        if not isinstance(self.rejection_reasons, tuple) or len(self.rejection_reasons) > len(
            RejectionReason
        ):
            raise ValueError("rejection reasons must be a bounded tuple")
        reasons = self.rejection_reasons
        if any(not isinstance(item, RejectionReason) for item in reasons):
            raise ValueError("rejection reasons must be declared")
        reasons = tuple(sorted(set(reasons), key=lambda item: item.value))
        if self.accepted != (not reasons):
            raise ValueError("accepted must be true exactly when rejection reasons are empty")
        if not isinstance(self.attestation_digests, tuple) or len(self.attestation_digests) > len(
            ApprovalRole
        ):
            raise ValueError("attestation digests must be a bounded tuple")
        attestations = self.attestation_digests
        if any(not isinstance(item, Digest) for item in attestations):
            raise ValueError("attestation digests must be canonical")
        attestations = tuple(sorted(set(attestations), key=lambda item: item.text))
        if self.authorization_scope != IMPLEMENTATION_ONLY:
            raise ValueError("scientific intake may authorize implementation only")
        if self.schema_version != SCIENTIFIC_INTAKE_DECISION_V1:
            raise ValueError("scientific intake decision schema version is unsupported")
        object.__setattr__(self, "rejection_reasons", reasons)
        object.__setattr__(self, "attestation_digests", attestations)

    def to_document(self) -> dict[str, object]:
        return {
            "schemaVersion": self.schema_version,
            "intakeDigest": self.intake_digest.text,
            "policyDigest": self.policy_digest.text,
            "accepted": self.accepted,
            "rejectionReasons": [item.value for item in self.rejection_reasons],
            "attestationDigests": [item.text for item in self.attestation_digests],
            "authorizationScope": self.authorization_scope,
        }

    def canonical_bytes(self) -> bytes:
        return canonical_json_bytes(self.to_document())

    @property
    def digest(self) -> Digest:
        return Digest.of(self.canonical_bytes())


def _contract_for(label: str) -> ArtifactContract:
    if label == "target-card":
        return TARGET_CARD_CONTRACT
    if label == "scientific-semantics":
        return SCIENTIFIC_SEMANTICS_CONTRACT
    if label == "preprocessing-contract":
        return PREPROCESSING_CONTRACT
    if label == "checkpoint-contract":
        return CHECKPOINT_CONTRACT
    if label == "reference-vector-pack":
        return REFERENCE_VECTOR_CONTRACT
    if label.startswith("training-dataset-") or label.startswith("evaluation-dataset-"):
        return DATASET_CONTRACT
    if label == "evaluation-policy":
        return EVALUATION_POLICY_CONTRACT
    if label == "serving-contract":
        return SERVING_CONTRACT
    if label.startswith("runtime-consumer-"):
        return RUNTIME_CONSUMER_CONTRACT
    if label == "safety-use-policy":
        return SAFETY_USE_CONTRACT
    if label.startswith("approval-"):
        return APPROVAL_CONTRACT
    raise ValueError(f"unsupported scientific intake artifact binding: {label}")


def _matches_contract(reference: ArtifactRef, contract: ArtifactContract) -> bool:
    return (
        reference.media_type == contract.media_type
        and reference.logical_kind == contract.logical_kind
        and reference.schema_version == contract.schema_version
    )


def evaluate_scientific_intake(
    intake: ScientificModelIntake,
    *,
    active_policy_digest: Digest,
    resolver: ScientificArtifactResolver,
) -> IntakeDecision:
    """Evaluate one bounded intake without side effects or provider fallback."""

    if not isinstance(intake, ScientificModelIntake):
        raise ValueError("intake must be a ScientificModelIntake")
    if not isinstance(active_policy_digest, Digest):
        raise ValueError("active_policy_digest must be canonical")
    if not callable(getattr(resolver, "resolve", None)):
        raise ValueError("resolver must implement resolve")

    reasons: set[RejectionReason] = set()
    if intake.purpose is not IntakePurpose.IMPLEMENTATION_AUTHORIZATION:
        reasons.add(RejectionReason.TEMPLATE_INTAKE)
    if intake.policy_digest != active_policy_digest:
        reasons.add(RejectionReason.POLICY_DIGEST_MISMATCH)
    if intake.reference_vector_pack is None:
        reasons.add(RejectionReason.MISSING_REFERENCE_VECTORS)
    if not intake.runtime_consumers:
        reasons.add(RejectionReason.RUNTIME_CONSUMER_REQUIRED)

    training_digests = {item.digest.text for item in intake.training_datasets}
    evaluation_digests = {item.digest.text for item in intake.evaluation_datasets}
    if training_digests & evaluation_digests:
        reasons.add(RejectionReason.TRAIN_EVALUATION_DATA_OVERLAP)

    bindings = intake.artifact_bindings()
    all_digests = [reference.digest.text for _, reference in bindings]
    if len(set(all_digests)) != len(all_digests):
        reasons.add(RejectionReason.DUPLICATE_ARTIFACT_DIGEST)

    approval_roles = [approval.role for approval in intake.approvals]
    if len(set(approval_roles)) != len(approval_roles):
        reasons.add(RejectionReason.DUPLICATE_APPROVAL_ROLE)
    required_roles = set(_BASE_APPROVALS)
    if intake.biosecurity_review_required:
        required_roles.add(ApprovalRole.BIOSECURITY)
    if not required_roles.issubset(set(approval_roles)):
        reasons.add(RejectionReason.MISSING_REQUIRED_APPROVAL)
    if any(approval.subject_digest != intake.target_card.digest for approval in intake.approvals):
        reasons.add(RejectionReason.APPROVAL_SUBJECT_MISMATCH)

    target_card: ModelTargetCard | None = None
    for label, reference in bindings:
        if not _matches_contract(reference, _contract_for(label)):
            reasons.add(RejectionReason.ARTIFACT_CONTRACT_MISMATCH)
        resolved = resolver.resolve(reference.digest)
        if resolved is None:
            reasons.add(RejectionReason.UNRESOLVED_ARTIFACT)
            continue
        if not isinstance(resolved, ResolvedArtifact):
            raise ValueError("resolver returned an invalid result")
        if resolved.reference != reference:
            reasons.add(RejectionReason.ARTIFACT_IDENTITY_MISMATCH)
            continue
        if label == "target-card":
            if resolved.document is None:
                reasons.add(RejectionReason.TARGET_CARD_INVALID)
                continue
            try:
                target_card = ModelTargetCard.from_document(resolved.document)
            except ValueError:
                reasons.add(RejectionReason.TARGET_CARD_INVALID)

    if target_card is not None:
        if target_card.schema_version != MODEL_TARGET_CARD_V2:
            reasons.add(RejectionReason.TARGET_CARD_V2_REQUIRED)
        if target_card.activation_state is not ActivationState.DESIGNED:
            reasons.add(RejectionReason.TARGET_CARD_MUST_BE_DESIGNED)
        if not target_card.safety_review_required:
            reasons.add(RejectionReason.TARGET_CARD_SAFETY_REVIEW_REQUIRED)
        if (
            set(target_card.training_dataset_digests) != training_digests
            or set(target_card.evaluation_dataset_digests) != evaluation_digests
        ):
            reasons.add(RejectionReason.TARGET_CARD_DATASET_MISMATCH)

    ordered_reasons = tuple(sorted(reasons, key=lambda item: item.value))
    attestation_digests = tuple(
        sorted(
            {approval.attestation.digest for approval in intake.approvals},
            key=lambda item: item.text,
        )
    )
    return IntakeDecision(
        intake_digest=intake.digest,
        policy_digest=active_policy_digest,
        accepted=not ordered_reasons,
        rejection_reasons=ordered_reasons,
        attestation_digests=attestation_digests,
    )
