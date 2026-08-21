# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed target cards shared by every model family."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass
from enum import StrEnum

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_IDENTIFIER = re.compile(r"[a-z][a-z0-9-]{1,62}")
_METRIC = re.compile(r"[a-z][a-z0-9_.-]{1,127}")


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


@dataclass(frozen=True)
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
        slices = tuple(self.required_slices)
        if (
            len(slices) > 64
            or any(not isinstance(item, str) for item in slices)
            or len(set(slices)) != len(slices)
        ):
            raise ValueError("required slices must be unique and bounded")
        if any(not _IDENTIFIER.fullmatch(item) for item in slices):
            raise ValueError("required slices must be stable identifiers")
        object.__setattr__(self, "required_slices", slices)


@dataclass(frozen=True)
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

    def __post_init__(self) -> None:
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
            materialized = tuple(values)
            if not materialized or len(materialized) > maximum:
                raise ValueError(f"{label} must be non-empty and bounded")
            if (
                any(not isinstance(item, str) for item in materialized)
                or len(set(materialized)) != len(materialized)
                or any(not _DIGEST.fullmatch(item) for item in materialized)
            ):
                raise ValueError(f"{label} must contain unique canonical digests")
            object.__setattr__(self, label, materialized)
        gates = tuple(self.metric_gates)
        if not gates or len(gates) > 128 or any(not isinstance(item, MetricGate) for item in gates):
            raise ValueError("metric_gates must contain 1..128 gates")
        if len({item.name for item in gates}) != len(gates):
            raise ValueError("metric gate names must be unique")
        declared_evaluation = set(self.evaluation_dataset_digests)
        if any(item.evaluation_dataset_digest not in declared_evaluation for item in gates):
            raise ValueError("every metric gate must bind a declared evaluation dataset")
        object.__setattr__(self, "metric_gates", gates)
        if not isinstance(self.availability_profile, str) or not _IDENTIFIER.fullmatch(
            self.availability_profile
        ):
            raise ValueError("availability_profile must be a stable identifier")
        for values, label in (
            (self.training_hardware_profiles, "training_hardware_profiles"),
            (self.serving_hardware_profiles, "serving_hardware_profiles"),
        ):
            materialized = tuple(values)
            if (
                not materialized
                or len(materialized) > 16
                or any(not isinstance(item, str) for item in materialized)
                or len(set(materialized)) != len(materialized)
            ):
                raise ValueError(f"{label} must be non-empty, unique, and bounded")
            if any(not _IDENTIFIER.fullmatch(item) for item in materialized):
                raise ValueError(f"{label} contains an invalid profile")
            object.__setattr__(self, label, materialized)
        if self.data_classification != "proprietary-internal":
            raise ValueError("current model targets are restricted to proprietary-internal data")
        if not isinstance(self.safety_review_required, bool):
            raise ValueError("safety_review_required must be a boolean")
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
