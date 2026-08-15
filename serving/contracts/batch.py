"""Tensor-aware batch planning contract owned by Python."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from .request import InferenceRequest


@dataclass(frozen=True, slots=True, order=True)
class CompatibilityKey:
    model_bundle_digest: str
    execution_kind: str
    precision: str
    shape_bucket: str

    def validate(self) -> None:
        if not self.model_bundle_digest.startswith("sha256:"):
            raise ValueError("batch model bundle digest is invalid")
        for value in (self.execution_kind, self.precision, self.shape_bucket):
            if not value or len(value) > 128:
                raise ValueError("batch compatibility key is invalid")


@dataclass(frozen=True, slots=True)
class BatchPlan:
    compatibility: CompatibilityKey
    requests: tuple[InferenceRequest, ...]
    estimated_gpu_bytes: int
    compilation_bucket: str

    def validate(self) -> None:
        self.compatibility.validate()
        if not self.requests:
            raise ValueError("batch plan is empty")
        if self.estimated_gpu_bytes <= 0:
            raise ValueError("batch GPU estimate must be positive")
        if not self.compilation_bucket or len(self.compilation_bucket) > 256:
            raise ValueError("batch compilation bucket is invalid")
        request_ids = [request.request_id for request in self.requests]
        if len(request_ids) != len(set(request_ids)):
            raise ValueError("batch plan contains duplicate requests")
        if any(
            request.model_bundle_digest != self.compatibility.model_bundle_digest
            for request in self.requests
        ):
            raise ValueError("batch plan mixes model bundles")


class BatchPlanner(Protocol):
    def plan(self, requests: tuple[InferenceRequest, ...]) -> tuple[BatchPlan, ...]: ...
