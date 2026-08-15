"""Bounded model-worker configuration."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ModelWorkerConfig:
    maximum_pending_requests: int = 1024
    maximum_batch_requests: int = 128
    maximum_gpu_bytes_per_batch: int = 80 * 1024**3

    def validate(self) -> None:
        if self.maximum_pending_requests <= 0:
            raise ValueError("pending-request limit must be positive")
        if (
            self.maximum_batch_requests <= 0
            or self.maximum_batch_requests > self.maximum_pending_requests
        ):
            raise ValueError("batch-request limit is invalid")
        if self.maximum_gpu_bytes_per_batch <= 0:
            raise ValueError("GPU batch budget must be positive")
