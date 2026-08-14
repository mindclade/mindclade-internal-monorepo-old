"""Model-neutral in-process contract between the Rust host and Python engine."""
from __future__ import annotations

from dataclasses import dataclass, field
from enum import StrEnum
from typing import Mapping


class WorkerPhase(StrEnum):
    STARTING = "starting"
    READY = "ready"
    DRAINING = "draining"
    STOPPED = "stopped"


@dataclass(frozen=True, slots=True)
class ModelRequest:
    request_id: str
    deployment_id: str
    model_bundle_digest: str
    precision_class: str
    execution_class: str
    input_units: int
    output_units: int
    payload_descriptor: str
    options: Mapping[str, str] = field(default_factory=dict)

    def validate(self, *, maximum_request_id_bytes: int = 128) -> None:
        if (
            not self.request_id
            or len(self.request_id.encode()) > maximum_request_id_bytes
            or not self.deployment_id
            or not self.model_bundle_digest.startswith("sha256:")
            or len(self.model_bundle_digest) != 71
            or not self.precision_class
            or not self.execution_class
            or self.input_units < 0
            or self.output_units < 0
            or not self.payload_descriptor
        ):
            raise ValueError("model request is incomplete or invalid")
        if len(self.options) > 128:
            raise ValueError("model request options exceed bound")
        for key, value in self.options.items():
            if not key or len(key) > 128 or len(value) > 4096:
                raise ValueError("model request option exceeds bound")


@dataclass(frozen=True, slots=True)
class ModelResponse:
    request_id: str
    payload: bytes = b""
    artifact_digest: str | None = None
    metrics: Mapping[str, float] = field(default_factory=dict)

    def validate(self) -> None:
        if not self.request_id or len(self.metrics) > 256:
            raise ValueError("model response is invalid")
        if self.artifact_digest is not None and (
            not self.artifact_digest.startswith("sha256:") or len(self.artifact_digest) != 71
        ):
            raise ValueError("model response artifact digest is invalid")
        for key, value in self.metrics.items():
            if not key or len(key) > 128 or not isinstance(value, (int, float)):
                raise ValueError("model response metric is invalid")
