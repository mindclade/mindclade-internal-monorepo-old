"""Process-local scientific stage contracts.

The canonical wire contract lives in ``protocols/proto/mindclade/orchestration/v1``.
Rust verifies signed execution authority before a Python engine is invoked; these
objects deliberately contain no signature or policy-verification logic.
"""

from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum

_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
_ID = re.compile(r"^[a-z][a-z0-9]{1,23}_[0-9a-f]{32}$")


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


@dataclass(frozen=True, slots=True)
class ArtifactRef:
    digest: str
    size_bytes: int
    media_type: str
    logical_kind: str
    schema_version: str

    def validate(self) -> None:
        if not _DIGEST.fullmatch(self.digest):
            raise ValueError("artifact digest must be canonical sha256")
        if (
            self.size_bytes < 0
            or not self.media_type
            or not self.logical_kind
            or not self.schema_version
        ):
            raise ValueError("artifact identity is incomplete")


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

    def validate(self) -> None:
        if not _ID.fullmatch(self.stage_id) or not self.operation or not self.output_namespace:
            raise ValueError("stage identity, operation, and output namespace are required")
        if not _DIGEST.fullmatch(self.resolved_config_digest):
            raise ValueError("resolved config digest must be canonical sha256")
        if self.reference_snapshot_digest is not None and not _DIGEST.fullmatch(
            self.reference_snapshot_digest
        ):
            raise ValueError("reference snapshot digest must be canonical sha256")
        if self.attempt <= 0 or self.fencing_token <= 0 or self.deadline_unix_millis <= 0:
            raise ValueError("attempt, fencing token, and deadline must be positive")
        if len(self.inputs) > 4096 or len(self.metadata) > 128:
            raise ValueError("stage envelope exceeds bounded collection limits")
        for artifact in self.inputs:
            artifact.validate()
        for key, value in self.metadata.items():
            if not key or len(key) > 128 or len(value) > 4096:
                raise ValueError("stage metadata exceeds bounds")


@dataclass(frozen=True, slots=True)
class StageResult:
    outputs: tuple[ArtifactRef, ...]
    metrics: Mapping[str, float] = field(default_factory=dict)

    def validate(self) -> None:
        if len(self.outputs) > 4096 or len(self.metrics) > 256:
            raise ValueError("stage result exceeds bounded collection limits")
        for artifact in self.outputs:
            artifact.validate()
        for key, value in self.metrics.items():
            if not key or len(key) > 128 or not isinstance(value, (int, float)):
                raise ValueError("stage metric is invalid")
