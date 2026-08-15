# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thin model-worker adapter over Python-owned batching and model engines."""

from __future__ import annotations

from typing import Protocol

from serving.contracts import BatchPlan, BatchPlanner, InferenceRequest, InferenceResult

from .config import ModelWorkerConfig
from .lifecycle import Lifecycle


class ModelEngine(Protocol):
    def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]: ...


class ModelWorker:
    def __init__(
        self,
        config: ModelWorkerConfig,
        planner: BatchPlanner,
        engine: ModelEngine,
        lifecycle: Lifecycle | None = None,
    ) -> None:
        config.validate()
        self._config = config
        self._planner = planner
        self._engine = engine
        self._lifecycle = lifecycle or Lifecycle()
        self._lifecycle.ready()

    @property
    def lifecycle(self) -> Lifecycle:
        return self._lifecycle

    def execute(
        self,
        requests: tuple[InferenceRequest, ...],
        *,
        now_unix_millis: int,
    ) -> tuple[InferenceResult, ...]:
        if not self._lifecycle.accepting():
            raise RuntimeError("model worker is draining")
        if not requests or len(requests) > self._config.maximum_pending_requests:
            raise ValueError("model-worker request count is outside bounds")
        for request in requests:
            request.validate(now_unix_millis)

        plans = self._planner.plan(requests)
        seen: set[str] = set()
        results: list[InferenceResult] = []
        for plan in plans:
            plan.validate()
            if len(plan.requests) > self._config.maximum_batch_requests:
                raise ValueError("tensor batch exceeds request bound")
            if plan.estimated_gpu_bytes > self._config.maximum_gpu_bytes_per_batch:
                raise ValueError("tensor batch exceeds GPU budget")
            for request in plan.requests:
                if request.request_id in seen:
                    raise ValueError("planner scheduled a request more than once")
                seen.add(request.request_id)
            batch_results = self._engine.execute(plan)
            for result in batch_results:
                result.validate()
                results.append(result)

        expected = {request.request_id for request in requests}
        produced = {result.request_id for result in results}
        if seen != expected or produced != expected:
            raise RuntimeError("planner/engine did not produce exactly one result per request")
        return tuple(results)
