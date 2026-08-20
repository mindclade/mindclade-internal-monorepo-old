# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded process-local projections of orchestration stage contracts.

The canonical wire contract lives in ``protocols/proto/mindclade/orchestration/v1``.
Rust verifies signed execution authority before a Python engine is invoked; these
objects contain no signature or policy-verification logic.
"""

from __future__ import annotations

import math
from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum
from types import MappingProxyType
from typing import Final

from libs.python.errors import InvalidArgument
from libs.python.identifiers import ArtifactRef, ResourceId, is_canonical_digest

MAXIMUM_INPUTS: Final = 4096
MAXIMUM_OUTPUTS: Final = 4096
MAXIMUM_METADATA_FIELDS: Final = 128
MAXIMUM_METRICS: Final = 256
MAXIMUM_NAME_LENGTH: Final = 128
MAXIMUM_METADATA_VALUE_LENGTH: Final = 4096
MAXIMUM_UINT32: Final = (1 << 32) - 1
MAXIMUM_UINT64: Final = (1 << 64) - 1


class StageKind(StrEnum):
    INGESTION = "ingestion"
    CURATE = "curate"
    PREPROCESS = "preprocess"
    REFERENCE_BUILD = "reference_build"
    BATCH_INFERENCE = "batch_inference"
    EVALUATION = "evaluation"
    TRAINING = "training"
    CHECKPOINT_TRANSFER = "checkpoint_transfer"
    ARTIFACT_TRANSFER = "artifact_transfer"
    ROLLOUT = "rollout"
    SIMULATION = "simulation"


def _bounded_text(value: object, *, name: str, maximum: int = MAXIMUM_NAME_LENGTH) -> str:
    if not isinstance(value, str) or not value or len(value) > maximum:
        raise InvalidArgument(
            f"{name} must be non-empty bounded text",
            reason="stage_text_field",
            fields={"field": name},
        )
    if any(ord(character) < 0x20 for character in value):
        raise InvalidArgument(
            f"{name} must not contain control characters",
            reason="stage_text_field",
            fields={"field": name},
        )
    return value


def _bounded_positive_integer(value: object, *, name: str, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= maximum:
        raise InvalidArgument(
            f"{name} must be a positive bounded integer",
            reason="stage_integer_field",
            fields={"field": name},
        )
    return value


def _resource_id(value: object, *, name: str, kind: str) -> str:
    if not isinstance(value, str):
        raise InvalidArgument(
            f"{name} must be a canonical resource ID",
            reason="stage_resource_id",
            fields={"field": name},
        )
    parsed = ResourceId.parse(value)
    if parsed.kind != kind:
        raise InvalidArgument(
            f"{name} must use resource kind {kind!r}",
            reason="stage_resource_kind",
            fields={"field": name, "expected_kind": kind},
        )
    return value


@dataclass(frozen=True, slots=True)
class StageEnvelope:
    stage_id: str
    kind: StageKind
    operation: str
    inputs: tuple[ArtifactRef, ...]
    output_namespace: str
    resolved_config_digest: str
    reference_snapshot_digest: str | None
    attempt: int
    fencing_token: int
    deadline_unix_millis: int
    metadata: Mapping[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        try:
            inputs = tuple(self.inputs)
        except TypeError as error:
            raise InvalidArgument(
                "stage inputs must be an iterable of ArtifactRef values",
                reason="stage_input_type",
                cause=error,
            ) from error
        object.__setattr__(self, "inputs", inputs)
        if not isinstance(self.metadata, Mapping):
            raise InvalidArgument("stage metadata must be a mapping", reason="stage_metadata_type")
        object.__setattr__(self, "metadata", MappingProxyType(dict(self.metadata)))
        self.validate()

    def validate(self) -> None:
        _resource_id(self.stage_id, name="stage_id", kind="stage")
        if not isinstance(self.kind, StageKind):
            raise InvalidArgument("stage kind is invalid", reason="stage_kind")
        _bounded_text(self.operation, name="operation")
        _bounded_text(self.output_namespace, name="output_namespace")
        if not isinstance(self.inputs, tuple) or len(self.inputs) > MAXIMUM_INPUTS:
            raise InvalidArgument(
                f"stage inputs must be a tuple with at most {MAXIMUM_INPUTS} entries",
                reason="stage_input_count",
            )
        if any(not isinstance(artifact, ArtifactRef) for artifact in self.inputs):
            raise InvalidArgument(
                "stage inputs must contain canonical ArtifactRef values",
                reason="stage_input_type",
            )
        if not is_canonical_digest(self.resolved_config_digest):
            raise InvalidArgument(
                "resolved config digest must be canonical sha256",
                reason="stage_config_digest",
            )
        if self.reference_snapshot_digest is not None and not is_canonical_digest(
            self.reference_snapshot_digest
        ):
            raise InvalidArgument(
                "reference snapshot digest must be canonical sha256",
                reason="stage_reference_digest",
            )
        _bounded_positive_integer(self.attempt, name="attempt", maximum=MAXIMUM_UINT32)
        _bounded_positive_integer(self.fencing_token, name="fencing_token", maximum=MAXIMUM_UINT64)
        _bounded_positive_integer(
            self.deadline_unix_millis,
            name="deadline_unix_millis",
            maximum=MAXIMUM_UINT64,
        )
        if not isinstance(self.metadata, Mapping) or len(self.metadata) > MAXIMUM_METADATA_FIELDS:
            raise InvalidArgument(
                f"stage metadata accepts at most {MAXIMUM_METADATA_FIELDS} fields",
                reason="stage_metadata_count",
            )
        for key, value in self.metadata.items():
            _bounded_text(key, name="metadata key")
            _bounded_text(value, name=f"metadata[{key}]", maximum=MAXIMUM_METADATA_VALUE_LENGTH)


@dataclass(frozen=True, slots=True)
class StageResult:
    outputs: tuple[ArtifactRef, ...]
    metrics: Mapping[str, float] = field(default_factory=dict)

    def __post_init__(self) -> None:
        try:
            outputs = tuple(self.outputs)
        except TypeError as error:
            raise InvalidArgument(
                "stage outputs must be an iterable of ArtifactRef values",
                reason="stage_output_type",
                cause=error,
            ) from error
        object.__setattr__(self, "outputs", outputs)
        if not isinstance(self.metrics, Mapping):
            raise InvalidArgument("stage metrics must be a mapping", reason="stage_metric_type")
        object.__setattr__(self, "metrics", MappingProxyType(dict(self.metrics)))
        self.validate()

    def validate(self) -> None:
        if not isinstance(self.outputs, tuple) or len(self.outputs) > MAXIMUM_OUTPUTS:
            raise InvalidArgument(
                f"stage outputs must be a tuple with at most {MAXIMUM_OUTPUTS} entries",
                reason="stage_output_count",
            )
        if any(not isinstance(artifact, ArtifactRef) for artifact in self.outputs):
            raise InvalidArgument(
                "stage outputs must contain canonical ArtifactRef values",
                reason="stage_output_type",
            )
        if not isinstance(self.metrics, Mapping) or len(self.metrics) > MAXIMUM_METRICS:
            raise InvalidArgument(
                f"stage metrics accept at most {MAXIMUM_METRICS} entries",
                reason="stage_metric_count",
            )
        for key, value in self.metrics.items():
            _bounded_text(key, name="metric name")
            try:
                is_finite = isinstance(value, int | float) and math.isfinite(value)
            except OverflowError:
                is_finite = False
            if isinstance(value, bool) or not is_finite:
                raise InvalidArgument(
                    "stage metric values must be finite numbers",
                    reason="stage_metric_value",
                    fields={"metric": key},
                )
