"""Strict process-local model-worker limits.

Global tenancy, routing and quota policy is intentionally absent. The Rust
runtime host admits work before this worker sees it; these limits protect the
Python process and final tensor-aware batch planner.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class WorkerLimits:
    maximum_batch_size: int = 32
    maximum_active_requests: int = 256
    maximum_input_units_per_batch: int = 1_000_000
    maximum_output_units_per_batch: int = 1_000_000
    maximum_request_id_bytes: int = 128

    def validate(self) -> None:
        values = (
            self.maximum_batch_size,
            self.maximum_active_requests,
            self.maximum_input_units_per_batch,
            self.maximum_output_units_per_batch,
            self.maximum_request_id_bytes,
        )
        if any(value <= 0 for value in values):
            raise ValueError("model-worker limits must be positive")
        if self.maximum_batch_size > self.maximum_active_requests:
            raise ValueError("batch size cannot exceed active-request limit")
        if self.maximum_batch_size > 4096 or self.maximum_active_requests > 65_536:
            raise ValueError("model-worker concurrency limits exceed hard safety bounds")
