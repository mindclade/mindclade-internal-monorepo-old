# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable inputs for authorizing scientific model implementation.

An accepted intake is deliberately weaker than model approval: it authorizes
engineering work against reviewed contracts and never mutates model registry or
maturity state. Every normative input is location-independent and content
addressed through the repository's canonical :class:`ArtifactRef`.
"""

from __future__ import annotations

import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from enum import StrEnum
from typing import Final

from libs.python.identifiers import ArtifactRef, Digest
from libs.python.serialization import canonical_json_bytes

SCIENTIFIC_MODEL_INTAKE_V1: Final = "mindclade.dev/scientific-model-intake/v1"
_INTAKE_FIELDS: Final = frozenset(
    {
        "schemaVersion",
        "intakeId",
        "purpose",
        "targetCard",
        "scientificSemantics",
        "preprocessingContract",
        "checkpointContract",
        "referenceVectorPack",
        "trainingDatasets",
        "evaluationDatasets",
        "evaluationPolicy",
        "servingContract",
        "runtimeConsumers",
        "safetyUsePolicy",
        "sourceRevision",
        "policyDigest",
        "biosecurityReviewRequired",
        "approvals",
    }
)
_APPROVAL_FIELDS: Final = frozenset({"role", "subjectDigest", "attestation"})
_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_SOURCE_REVISION = re.compile(r"[0-9a-f]{40}")


class IntakePurpose(StrEnum):
    IMPLEMENTATION_AUTHORIZATION = "implementation-authorization"
    TEMPLATE = "template"


class ApprovalRole(StrEnum):
    MODELING = "modeling"
    DATA = "data"
    EVALUATION_TRAINING = "evaluation-training"
    RUNTIME = "runtime"
    PLATFORM_CONTROL = "platform-control"
    RELEASE = "release"
    SECURITY = "security"
    BIOSECURITY = "biosecurity"


def _closed_mapping(value: object, fields: frozenset[str], label: str) -> Mapping[str, object]:
    if not isinstance(value, Mapping) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"{label} must be a string-keyed object")
    if set(value) != fields:
        raise ValueError(f"{label} has unknown or missing fields")
    return value


def _sequence(value: object, label: str, *, maximum: int) -> tuple[object, ...]:
    if not isinstance(value, Sequence) or isinstance(value, str | bytes | bytearray):
        raise ValueError(f"{label} must be an array")
    if len(value) > maximum:
        raise ValueError(f"{label} exceeds its bounded count")
    return tuple(value)


def _artifact(value: object, label: str) -> ArtifactRef:
    if not isinstance(value, Mapping):
        raise ValueError(f"{label} must be an ArtifactRef object")
    try:
        return ArtifactRef.from_document(value)
    except ValueError as error:
        raise ValueError(f"{label} must be a canonical ArtifactRef") from error


def _artifacts(value: object, label: str, *, maximum: int) -> tuple[ArtifactRef, ...]:
    return tuple(_artifact(item, label) for item in _sequence(value, label, maximum=maximum))


def _digest(value: object, label: str) -> Digest:
    if not isinstance(value, str):
        raise ValueError(f"{label} must be a canonical digest")
    try:
        return Digest.parse(value)
    except ValueError as error:
        raise ValueError(f"{label} must be a canonical digest") from error


def _enum_value[T: StrEnum](kind: type[T], value: object, label: str) -> T:
    if not isinstance(value, str):
        raise ValueError(f"{label} must be text")
    try:
        return kind(value)
    except ValueError as error:
        raise ValueError(f"{label} is outside the supported values") from error


@dataclass(frozen=True, slots=True)
class ApprovalAttestation:
    """One role's immutable approval of the target card under the intake policy."""

    role: ApprovalRole
    subject_digest: Digest
    attestation: ArtifactRef

    def __post_init__(self) -> None:
        if not isinstance(self.role, ApprovalRole):
            raise ValueError("approval role must be declared")
        if not isinstance(self.subject_digest, Digest):
            raise ValueError("approval subject must be a canonical digest")
        if not isinstance(self.attestation, ArtifactRef):
            raise ValueError("approval must reference an immutable attestation")

    @classmethod
    def from_document(cls, document: object) -> ApprovalAttestation:
        value = _closed_mapping(document, _APPROVAL_FIELDS, "approval")
        return cls(
            role=_enum_value(ApprovalRole, value["role"], "approval role"),
            subject_digest=_digest(value["subjectDigest"], "approval subjectDigest"),
            attestation=_artifact(value["attestation"], "approval attestation"),
        )

    def to_document(self) -> dict[str, object]:
        return {
            "role": self.role.value,
            "subjectDigest": self.subject_digest.text,
            "attestation": self.attestation.to_document(),
        }


@dataclass(frozen=True, slots=True)
class ScientificModelIntake:
    """A closed, bounded proposal for implementation-only authorization."""

    intake_id: str
    purpose: IntakePurpose
    target_card: ArtifactRef
    scientific_semantics: ArtifactRef
    preprocessing_contract: ArtifactRef
    checkpoint_contract: ArtifactRef
    reference_vector_pack: ArtifactRef | None
    training_datasets: tuple[ArtifactRef, ...]
    evaluation_datasets: tuple[ArtifactRef, ...]
    evaluation_policy: ArtifactRef
    serving_contract: ArtifactRef
    runtime_consumers: tuple[ArtifactRef, ...]
    safety_use_policy: ArtifactRef
    source_revision: str
    policy_digest: Digest
    biosecurity_review_required: bool
    approvals: tuple[ApprovalAttestation, ...]
    schema_version: str = SCIENTIFIC_MODEL_INTAKE_V1

    def __post_init__(self) -> None:
        if self.schema_version != SCIENTIFIC_MODEL_INTAKE_V1:
            raise ValueError("scientific intake schema version is unsupported")
        if not isinstance(self.intake_id, str) or not _IDENTIFIER.fullmatch(self.intake_id):
            raise ValueError("intake_id must be a stable bounded identifier")
        if not isinstance(self.purpose, IntakePurpose):
            raise ValueError("intake purpose must be declared")
        for value, label in (
            (self.target_card, "target_card"),
            (self.scientific_semantics, "scientific_semantics"),
            (self.preprocessing_contract, "preprocessing_contract"),
            (self.checkpoint_contract, "checkpoint_contract"),
            (self.evaluation_policy, "evaluation_policy"),
            (self.serving_contract, "serving_contract"),
            (self.safety_use_policy, "safety_use_policy"),
        ):
            if not isinstance(value, ArtifactRef):
                raise ValueError(f"{label} must be an ArtifactRef")
        if self.reference_vector_pack is not None and not isinstance(
            self.reference_vector_pack, ArtifactRef
        ):
            raise ValueError("reference_vector_pack must be an ArtifactRef or None")
        for values, label, minimum, maximum in (
            (self.training_datasets, "training_datasets", 1, 64),
            (self.evaluation_datasets, "evaluation_datasets", 1, 32),
            (self.runtime_consumers, "runtime_consumers", 0, 16),
        ):
            materialized = _sequence(values, label, maximum=maximum)
            if not minimum <= len(materialized) <= maximum:
                raise ValueError(f"{label} is outside its bounded count")
            if any(not isinstance(item, ArtifactRef) for item in materialized):
                raise ValueError(f"{label} must contain ArtifactRef values")
            object.__setattr__(
                self,
                label,
                tuple(sorted(materialized, key=lambda item: item.digest.text)),
            )
        if not isinstance(self.source_revision, str) or not _SOURCE_REVISION.fullmatch(
            self.source_revision
        ):
            raise ValueError("source_revision must be a full lowercase Git commit")
        if not isinstance(self.policy_digest, Digest):
            raise ValueError("policy_digest must be a canonical digest")
        if not isinstance(self.biosecurity_review_required, bool):
            raise ValueError("biosecurity_review_required must be a boolean")
        approvals = _sequence(self.approvals, "approvals", maximum=len(ApprovalRole))
        if len(approvals) > len(ApprovalRole) or any(
            not isinstance(item, ApprovalAttestation) for item in approvals
        ):
            raise ValueError("approvals must contain at most one entry per declared role")
        object.__setattr__(
            self,
            "approvals",
            tuple(
                sorted(approvals, key=lambda item: (item.role.value, item.attestation.digest.text))
            ),
        )

    @classmethod
    def from_document(cls, document: object) -> ScientificModelIntake:
        value = _closed_mapping(document, _INTAKE_FIELDS, "scientific model intake")
        if value["schemaVersion"] != SCIENTIFIC_MODEL_INTAKE_V1:
            raise ValueError("scientific intake schemaVersion is unsupported")
        reference_vector = value["referenceVectorPack"]
        if reference_vector is not None and not isinstance(reference_vector, Mapping):
            raise ValueError("referenceVectorPack must be an ArtifactRef or null")
        intake_id = value["intakeId"]
        source_revision = value["sourceRevision"]
        biosecurity_review_required = value["biosecurityReviewRequired"]
        if not isinstance(intake_id, str):
            raise ValueError("intakeId must be text")
        if not isinstance(source_revision, str):
            raise ValueError("sourceRevision must be text")
        if not isinstance(biosecurity_review_required, bool):
            raise ValueError("biosecurityReviewRequired must be a boolean")
        return cls(
            intake_id=intake_id,
            purpose=_enum_value(IntakePurpose, value["purpose"], "purpose"),
            target_card=_artifact(value["targetCard"], "targetCard"),
            scientific_semantics=_artifact(value["scientificSemantics"], "scientificSemantics"),
            preprocessing_contract=_artifact(
                value["preprocessingContract"], "preprocessingContract"
            ),
            checkpoint_contract=_artifact(value["checkpointContract"], "checkpointContract"),
            reference_vector_pack=(
                None
                if reference_vector is None
                else _artifact(reference_vector, "referenceVectorPack")
            ),
            training_datasets=_artifacts(value["trainingDatasets"], "trainingDatasets", maximum=64),
            evaluation_datasets=_artifacts(
                value["evaluationDatasets"], "evaluationDatasets", maximum=32
            ),
            evaluation_policy=_artifact(value["evaluationPolicy"], "evaluationPolicy"),
            serving_contract=_artifact(value["servingContract"], "servingContract"),
            runtime_consumers=_artifacts(value["runtimeConsumers"], "runtimeConsumers", maximum=16),
            safety_use_policy=_artifact(value["safetyUsePolicy"], "safetyUsePolicy"),
            source_revision=source_revision,
            policy_digest=_digest(value["policyDigest"], "policyDigest"),
            biosecurity_review_required=biosecurity_review_required,
            approvals=tuple(
                ApprovalAttestation.from_document(item)
                for item in _sequence(value["approvals"], "approvals", maximum=len(ApprovalRole))
            ),
        )

    def to_document(self) -> dict[str, object]:
        return {
            "schemaVersion": self.schema_version,
            "intakeId": self.intake_id,
            "purpose": self.purpose.value,
            "targetCard": self.target_card.to_document(),
            "scientificSemantics": self.scientific_semantics.to_document(),
            "preprocessingContract": self.preprocessing_contract.to_document(),
            "checkpointContract": self.checkpoint_contract.to_document(),
            "referenceVectorPack": (
                None
                if self.reference_vector_pack is None
                else self.reference_vector_pack.to_document()
            ),
            "trainingDatasets": [item.to_document() for item in self.training_datasets],
            "evaluationDatasets": [item.to_document() for item in self.evaluation_datasets],
            "evaluationPolicy": self.evaluation_policy.to_document(),
            "servingContract": self.serving_contract.to_document(),
            "runtimeConsumers": [item.to_document() for item in self.runtime_consumers],
            "safetyUsePolicy": self.safety_use_policy.to_document(),
            "sourceRevision": self.source_revision,
            "policyDigest": self.policy_digest.text,
            "biosecurityReviewRequired": self.biosecurity_review_required,
            "approvals": [item.to_document() for item in self.approvals],
        }

    def canonical_bytes(self) -> bytes:
        return canonical_json_bytes(self.to_document())

    @property
    def digest(self) -> Digest:
        return Digest.of(self.canonical_bytes())

    def artifact_bindings(self) -> tuple[tuple[str, ArtifactRef], ...]:
        """Return every declared immutable edge with a stable diagnostic label."""

        bindings: list[tuple[str, ArtifactRef]] = [
            ("target-card", self.target_card),
            ("scientific-semantics", self.scientific_semantics),
            ("preprocessing-contract", self.preprocessing_contract),
            ("checkpoint-contract", self.checkpoint_contract),
            ("evaluation-policy", self.evaluation_policy),
            ("serving-contract", self.serving_contract),
            ("safety-use-policy", self.safety_use_policy),
        ]
        if self.reference_vector_pack is not None:
            bindings.append(("reference-vector-pack", self.reference_vector_pack))
        bindings.extend(
            (f"training-dataset-{index}", value)
            for index, value in enumerate(self.training_datasets)
        )
        bindings.extend(
            (f"evaluation-dataset-{index}", value)
            for index, value in enumerate(self.evaluation_datasets)
        )
        bindings.extend(
            (f"runtime-consumer-{index}", value)
            for index, value in enumerate(self.runtime_consumers)
        )
        bindings.extend(
            (f"approval-{approval.role.value}", approval.attestation) for approval in self.approvals
        )
        return tuple(bindings)
