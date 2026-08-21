# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral batch execution over a model engine."""

from __future__ import annotations

from collections.abc import Callable
from typing import Protocol

from libs.python.worker_runtime import CancellationToken
from serving.contracts import InferenceRequest, InferenceResult

from .batching import BatchSlice, partition
from .config import BatchLimits
from .job import BatchJob
from .result import BatchResult


class BatchEngine(Protocol):
    def execute(
        self, batch: BatchSlice, cancellation: CancellationToken
    ) -> tuple[InferenceResult, ...]: ...


class BatchExecutor:
    def __init__(
        self,
        limits: BatchLimits,
        engine: BatchEngine,
        *,
        estimate_bytes: Callable[[InferenceRequest], int],
    ) -> None:
        if not callable(getattr(engine, "execute", None)) or not callable(estimate_bytes):
            raise ValueError("batch engine and estimator must be callable")
        self._limits = limits
        self._engine = engine
        self._estimate_bytes = estimate_bytes

    def execute(
        self,
        job: BatchJob,
        *,
        now_unix_millis: int,
        cancellation: CancellationToken,
    ) -> BatchResult:
        job.validate(self._limits, now_unix_millis=now_unix_millis)
        batches = partition(job, self._limits, estimate_bytes=self._estimate_bytes)
        results: list[InferenceResult] = []
        for batch in batches:
            if cancellation.is_cancelled:
                raise RuntimeError("batch job was canceled")
            batch_results = self._engine.execute(batch, cancellation)
            expected = tuple(request.request_id for request in batch.requests)
            actual = tuple(result.request_id for result in batch_results)
            if actual != expected:
                raise RuntimeError("batch engine violated request order/cardinality")
            for result in batch_results:
                result.validate()
                if result.model_bundle_digest != job.model_bundle_digest:
                    raise RuntimeError("batch engine returned a result from another model bundle")
            results.extend(batch_results)
        outcome = BatchResult(job.job_id, job.fencing_token, tuple(results), len(batches))
        outcome.validate(tuple(request.request_id for request in job.requests))
        return outcome
