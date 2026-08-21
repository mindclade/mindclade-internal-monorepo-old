# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable, deterministic partitioning for an already-admitted batch job."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from serving.contracts import InferenceRequest

from .config import BatchLimits
from .job import BatchJob


@dataclass(frozen=True, slots=True)
class BatchSlice:
    ordinal: int
    requests: tuple[InferenceRequest, ...]
    total_units: int
    estimated_bytes: int


def partition(
    job: BatchJob,
    limits: BatchLimits,
    *,
    estimate_bytes: Callable[[InferenceRequest], int],
) -> tuple[BatchSlice, ...]:
    """Partition in input order; an individual oversize request fails closed."""
    batches: list[BatchSlice] = []
    pending: list[InferenceRequest] = []
    pending_units = 0
    pending_bytes = 0

    def flush() -> None:
        nonlocal pending, pending_units, pending_bytes
        if pending:
            batches.append(BatchSlice(len(batches), tuple(pending), pending_units, pending_bytes))
            pending = []
            pending_units = 0
            pending_bytes = 0

    for request in job.requests:
        units = request.input_units + request.output_units
        estimated = estimate_bytes(request)
        if isinstance(estimated, bool) or not isinstance(estimated, int) or estimated <= 0:
            raise ValueError("batch byte estimator returned an invalid value")
        if units > limits.maximum_units_per_batch:
            raise ValueError(f"request {request.request_id!r} exceeds the unit ceiling")
        if estimated > limits.maximum_estimated_bytes_per_batch:
            raise ValueError(f"request {request.request_id!r} exceeds the byte ceiling")
        would_overflow = pending and (
            len(pending) == limits.maximum_requests_per_batch
            or pending_units + units > limits.maximum_units_per_batch
            or pending_bytes + estimated > limits.maximum_estimated_bytes_per_batch
        )
        if would_overflow:
            flush()
        pending.append(request)
        pending_units += units
        pending_bytes += estimated
    flush()
    return tuple(batches)
