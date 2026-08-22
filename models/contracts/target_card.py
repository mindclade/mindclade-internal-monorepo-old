# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed target cards shared by every model family.

Version 2 is the canonical write contract. Version 1 remains a bounded read
compatibility seam for already-materialized cards; it cannot express required
evaluation slices or an explicit safety-review policy and is therefore not
eligible for new scientific intake decisions.
"""

from __future__ import annotations

import math
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from enum import StrEnum
from typing import Final, cast

MODEL_TARGET_CARD_V1: Final = "mindclade.dev/model-target-card/v1"
MODEL_TARGET_CARD_V2: Final = "mindclade.dev/model-target-card/v2"

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_METRIC = re.compile(r"[a-z][a-z0-9_.-]{1,127}")
_METRIC_V1_FIELDS: Final = frozenset(
    {"name", "direction", "threshold", "minimumSamples", "evaluationDatasetDigest"}
)
_METRIC_V2_FIELDS: Final = _METRIC_V1_FIELDS | {"requiredSlices"}
_CARD_V1_FIELDS: Final = frozenset(
    {
        "schemaVersion",
        "modelName",
        "family",
        "owner",
        "dataClassification",
        "inputSchemaDigest",
        "outputSchemaDigest",
        "trainingDatasetDigests",
        "evaluationDatasetDigests",
        "metricGates",
        "availabilityProfile",
        "trainingHardwareProfiles",
        "servingHardwareProfiles",
        "activation",
    }
)
_CARD_V2_FIELDS: Final = _CARD_V1_FIELDS | {"safetyReviewRequired"}
_ACTIVATION_FIELDS: Final = frozenset({"state", "qualificationEvidenceDigest"})


class ModelFamily(StrEnum):
    BIOLOGY = "biology"
    DIFFUSION = "diffusion"
    LLM = "llm"
    MOE = "moe"
    MULTIMODAL = "multimodal"


class MetricDirection(StrEnum):
    AT_LEAST = "at-least"
    AT_MOST = "at-most"


class ActivationState(StrEnum):
    DESIGNED = "designed"
    QUALIFICATION = "qualification"
    APPROVED = "approved"


def _closed_mapping(value: object, fields: frozenset[str], label: str) -> Mapping[str, object]:
    if not isinstance(value, Mapping) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"{label} must be a string-keyed object")
    if set(value) != fields:
        raise ValueError(f"{label} has unknown or missing fields")
    return value


def _sequence(value: object, label: str, *, maximum: int | None = None) -> tuple[object, ...]:
    if not isinstance(value, Sequence) or isinstance(value, str | bytes | bytearray):
        raise ValueError(f"{label} must be an array")
    if maximum is not None and len(value) > maximum:
        raise ValueError(f"{label} exceeds its bounded count")
    return tuple(value)


def _text(value: object, label: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{label} must be text")
    return value


def _string_tuple(value: object, label: str, *, maximum: int) -> tuple[str, ...]:
    values = _sequence(value, label, maximum=maximum)
    if any(not isinstance(item, str) for item in values):
        raise ValueError(f"{label} must contain text values")
    return tuple(item for item in values if isinstance(item, str))


def _enum_value[T: StrEnum](kind: type[T], value: object, label: str) -> T:
    if not isinstance(value, str):
        raise ValueError(f"{label} must be text")
    try:
        return kind(value)
    except ValueError as error:
        raise ValueError(f"{label} is outside the supported values") from error


@dataclass(frozen=True, slots=True)
class MetricGate:
    name: str
    direction: MetricDirection
    threshold: float
    minimum_samples: int
    evaluation_dataset_digest: str
    required_slices: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _METRIC.fullmatch(self.name):
            raise ValueError("metric name must be a bounded stable identifier")
        if not isinstance(self.direction, MetricDirection):
            raise ValueError("metric direction must be explicit")
        if isinstance(self.threshold, bool) or not isinstance(self.threshold, float | int):
            raise ValueError("metric threshold must be numeric")
        if not math.isfinite(float(self.threshold)):
            raise ValueError("metric threshold must be finite")
        if (
            isinstance(self.minimum_samples, bool)
            or not isinstance(self.minimum_samples, int)
            or not 1 <= self.minimum_samples <= 1_000_000_000
        ):
            raise ValueError("minimum_samples is outside its bounded range")
        if not isinstance(self.evaluation_dataset_digest, str) or not _DIGEST.fullmatch(
            self.evaluation_dataset_digest
        ):
            raise ValueError("metric gate must bind an immutable evaluation dataset")
        slices = _sequence(self.required_slices, "required slices", maximum=64)
        if (
            len(slices) > 64
            or any(not isinstance(item, str) for item in slices)
            or len(set(slices)) != len(slices)
        ):
            raise ValueError("required slices must be unique and bounded")
        slices = cast(tuple[str, ...], slices)
        if any(not _IDENTIFIER.fullmatch(item) for item in slices):
            raise ValueError("required slices must be stable identifiers")
        object.__setattr__(self, "required_slices", slices)

    @classmethod
    def from_document(cls, document: object, *, schema_version: str) -> MetricGate:
        fields = _METRIC_V1_FIELDS if schema_version == MODEL_TARGET_CARD_V1 else _METRIC_V2_FIELDS
        value = _closed_mapping(document, fields, "metric gate")
        threshold = value["threshold"]
        minimum_samples = value["minimumSamples"]
        if isinstance(threshold, bool) or not isinstance(threshold, float | int):
            raise ValueError("metric threshold must be numeric")
        if isinstance(minimum_samples, bool) or not isinstance(minimum_samples, int):
            raise ValueError("minimumSamples must be an integer")
        required_slices = (
            ()
            if schema_version == MODEL_TARGET_CARD_V1
            else _string_tuple(value["requiredSlices"], "requiredSlices", maximum=64)
        )
        return cls(
            name=_text(value["name"], "metric name"),
            direction=_enum_value(MetricDirection, value["direction"], "metric direction"),
            threshold=threshold,
            minimum_samples=minimum_samples,
            evaluation_dataset_digest=_text(
                value["evaluationDatasetDigest"], "evaluationDatasetDigest"
            ),
            required_slices=required_slices,
        )

    def to_document(self, *, schema_version: str) -> dict[str, object]:
        document: dict[str, object] = {
            "name": self.name,
            "direction": self.direction.value,
            "threshold": self.threshold,
            "minimumSamples": self.minimum_samples,
            "evaluationDatasetDigest": self.evaluation_dataset_digest,
        }
        if schema_version == MODEL_TARGET_CARD_V2:
            document["requiredSlices"] = list(self.required_slices)
        return document


@dataclass(frozen=True, slots=True)
class ModelTargetCard:
    """One reviewable model target without claiming qualification by file presence."""

    model_name: str
    family: ModelFamily
    owner: str
    input_schema_digest: str
    output_schema_digest: str
    training_dataset_digests: tuple[str, ...]
    evaluation_dataset_digests: tuple[str, ...]
    metric_gates: tuple[MetricGate, ...]
    availability_profile: str
    training_hardware_profiles: tuple[str, ...]
    serving_hardware_profiles: tuple[str, ...]
    activation_state: ActivationState = ActivationState.DESIGNED
    qualification_evidence_digest: str | None = None
    data_classification: str = "proprietary-internal"
    safety_review_required: bool = True
    schema_version: str = MODEL_TARGET_CARD_V2

    def __post_init__(self) -> None:
        if self.schema_version not in {MODEL_TARGET_CARD_V1, MODEL_TARGET_CARD_V2}:
            raise ValueError("schema_version must name a supported target-card contract")
        if (
            not isinstance(self.model_name, str)
            or not _IDENTIFIER.fullmatch(self.model_name)
            or not isinstance(self.owner, str)
            or not _IDENTIFIER.fullmatch(self.owner)
        ):
            raise ValueError("model_name and owner must be stable bounded identifiers")
        if not isinstance(self.family, ModelFamily):
            raise ValueError("family must be a declared ModelFamily")
        for value, label in (
            (self.input_schema_digest, "input_schema_digest"),
            (self.output_schema_digest, "output_schema_digest"),
        ):
            if not isinstance(value, str) or not _DIGEST.fullmatch(value):
                raise ValueError(f"{label} must be canonical SHA-256")
        for values, label, maximum in (
            (self.training_dataset_digests, "training_dataset_digests", 64),
            (self.evaluation_dataset_digests, "evaluation_dataset_digests", 32),
        ):
            materialized = _sequence(values, label, maximum=maximum)
            if not materialized or len(materialized) > maximum:
                raise ValueError(f"{label} must be non-empty and bounded")
            if any(not isinstance(item, str) for item in materialized) or len(
                set(materialized)
            ) != len(materialized):
                raise ValueError(f"{label} must contain unique canonical digests")
            materialized = cast(tuple[str, ...], materialized)
            if any(not _DIGEST.fullmatch(item) for item in materialized):
                raise ValueError(f"{label} must contain unique canonical digests")
            object.__setattr__(self, label, materialized)
        gates = _sequence(self.metric_gates, "metric_gates", maximum=128)
        if not gates or len(gates) > 128 or any(not isinstance(item, MetricGate) for item in gates):
            raise ValueError("metric_gates must contain 1..128 gates")
        gates = cast(tuple[MetricGate, ...], gates)
        if len({item.name for item in gates}) != len(gates):
            raise ValueError("metric gate names must be unique")
        declared_evaluation = set(self.evaluation_dataset_digests)
        if any(item.evaluation_dataset_digest not in declared_evaluation for item in gates):
            raise ValueError("every metric gate must bind a declared evaluation dataset")
        if self.schema_version == MODEL_TARGET_CARD_V2 and any(
            not item.required_slices for item in gates
        ):
            raise ValueError("v2 metric gates must predeclare at least one required slice")
        if self.schema_version == MODEL_TARGET_CARD_V1 and any(
            item.required_slices for item in gates
        ):
            raise ValueError("v1 metric gates cannot represent required slices")
        object.__setattr__(self, "metric_gates", gates)
        if not isinstance(self.availability_profile, str) or not _IDENTIFIER.fullmatch(
            self.availability_profile
        ):
            raise ValueError("availability_profile must be a stable identifier")
        for values, label in (
            (self.training_hardware_profiles, "training_hardware_profiles"),
            (self.serving_hardware_profiles, "serving_hardware_profiles"),
        ):
            materialized = _sequence(values, label, maximum=16)
            if (
                not materialized
                or len(materialized) > 16
                or any(not isinstance(item, str) for item in materialized)
                or len(set(materialized)) != len(materialized)
            ):
                raise ValueError(f"{label} must be non-empty, unique, and bounded")
            materialized = cast(tuple[str, ...], materialized)
            if any(not _IDENTIFIER.fullmatch(item) for item in materialized):
                raise ValueError(f"{label} contains an invalid profile")
            object.__setattr__(self, label, materialized)
        if self.data_classification != "proprietary-internal":
            raise ValueError("current model targets are restricted to proprietary-internal data")
        if not isinstance(self.safety_review_required, bool):
            raise ValueError("safety_review_required must be a boolean")
        if self.schema_version == MODEL_TARGET_CARD_V1 and not self.safety_review_required:
            raise ValueError("v1 cards cannot represent a disabled safety review")
        if not isinstance(self.activation_state, ActivationState):
            raise ValueError("activation_state must be an ActivationState")
        if self.activation_state is ActivationState.APPROVED:
            if not self.safety_review_required:
                raise ValueError("approved targets must require safety review")
            if not isinstance(self.qualification_evidence_digest, str) or not _DIGEST.fullmatch(
                self.qualification_evidence_digest
            ):
                raise ValueError("approved targets require immutable qualification evidence")
        elif self.qualification_evidence_digest is not None:
            raise ValueError("unapproved targets may not claim qualification evidence")

    @classmethod
    def from_document(cls, document: object) -> ModelTargetCard:
        if not isinstance(document, Mapping):
            raise ValueError("target card must be an object")
        schema_version = document.get("schemaVersion")
        if schema_version == MODEL_TARGET_CARD_V1:
            fields = _CARD_V1_FIELDS
        elif schema_version == MODEL_TARGET_CARD_V2:
            fields = _CARD_V2_FIELDS
        else:
            raise ValueError("target card schemaVersion is unsupported")
        value = _closed_mapping(document, fields, "target card")
        activation = _closed_mapping(value["activation"], _ACTIVATION_FIELDS, "activation")
        gate_values = _sequence(value["metricGates"], "metricGates", maximum=128)
        qualification_digest = activation["qualificationEvidenceDigest"]
        if qualification_digest is not None and not isinstance(qualification_digest, str):
            raise ValueError("qualificationEvidenceDigest must be text or null")
        safety_review_required = (
            True if schema_version == MODEL_TARGET_CARD_V1 else value["safetyReviewRequired"]
        )
        if not isinstance(safety_review_required, bool):
            raise ValueError("safetyReviewRequired must be a boolean")
        return cls(
            model_name=_text(value["modelName"], "modelName"),
            family=_enum_value(ModelFamily, value["family"], "family"),
            owner=_text(value["owner"], "owner"),
            input_schema_digest=_text(value["inputSchemaDigest"], "inputSchemaDigest"),
            output_schema_digest=_text(value["outputSchemaDigest"], "outputSchemaDigest"),
            training_dataset_digests=_string_tuple(
                value["trainingDatasetDigests"], "trainingDatasetDigests", maximum=64
            ),
            evaluation_dataset_digests=_string_tuple(
                value["evaluationDatasetDigests"], "evaluationDatasetDigests", maximum=32
            ),
            metric_gates=tuple(
                MetricGate.from_document(item, schema_version=schema_version)
                for item in gate_values
            ),
            availability_profile=_text(value["availabilityProfile"], "availabilityProfile"),
            training_hardware_profiles=_string_tuple(
                value["trainingHardwareProfiles"], "trainingHardwareProfiles", maximum=16
            ),
            serving_hardware_profiles=_string_tuple(
                value["servingHardwareProfiles"], "servingHardwareProfiles", maximum=16
            ),
            activation_state=_enum_value(ActivationState, activation["state"], "activation state"),
            qualification_evidence_digest=qualification_digest,
            data_classification=_text(value["dataClassification"], "dataClassification"),
            safety_review_required=safety_review_required,
            schema_version=schema_version,
        )

    def to_document(self) -> dict[str, object]:
        document: dict[str, object] = {
            "schemaVersion": self.schema_version,
            "modelName": self.model_name,
            "family": self.family.value,
            "owner": self.owner,
            "dataClassification": self.data_classification,
            "inputSchemaDigest": self.input_schema_digest,
            "outputSchemaDigest": self.output_schema_digest,
            "trainingDatasetDigests": list(self.training_dataset_digests),
            "evaluationDatasetDigests": list(self.evaluation_dataset_digests),
            "metricGates": [
                gate.to_document(schema_version=self.schema_version) for gate in self.metric_gates
            ],
            "availabilityProfile": self.availability_profile,
            "trainingHardwareProfiles": list(self.training_hardware_profiles),
            "servingHardwareProfiles": list(self.serving_hardware_profiles),
            "activation": {
                "state": self.activation_state.value,
                "qualificationEvidenceDigest": self.qualification_evidence_digest,
            },
        }
        if self.schema_version == MODEL_TARGET_CARD_V2:
            document["safetyReviewRequired"] = self.safety_review_required
        return document
