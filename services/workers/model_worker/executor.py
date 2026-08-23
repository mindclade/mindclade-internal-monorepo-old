# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thin model-worker adapter over Python-owned batching and model engines."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from threading import Semaphore
from typing import Protocol

from libs.python.errors import (
    FailedPrecondition,
    InvalidArgument,
    MindcladeError,
    ResourceExhausted,
)
from libs.python.worker_runtime import WorkerLimits
from serving.contracts import BatchPlan, BatchPlanner, InferenceRequest, InferenceResult

from .config import ModelWorkerConfig
from .lifecycle import Lifecycle


class ModelEngine(Protocol):
    def execute(self, batch: BatchPlan) -> tuple[InferenceResult, ...]: ...


@contextmanager
def _contract_failures_as(
    error_type: type[MindcladeError],
    message: str,
    *,
    reason: str,
) -> Iterator[None]:
    """Re-raise a bare ``serving.contracts`` rejection as the shared error contract.

    ``serving.contracts`` and ``config.py`` validate with plain ``ValueError``. Those cover the
    most ordinary rejections there are -- an expired request deadline or input lease -- and they
    used to cross the Rust supervision boundary untyped, so nothing downstream could classify or
    route them alongside the errors this adapter raises itself.
    """

    try:
        yield
    except MindcladeError:
        raise
    except ValueError as error:
        raise error_type(message, reason=reason, cause=error) from error


class ModelWorker:
    """Bounded adapter that admits, plans, and executes one call's worth of requests.

    ``limits`` is the same defence-in-depth object the stage workers use, and it is enforced
    rather than merely accepted: ``maximum_concurrent_executions`` is held by a non-blocking
    semaphore, so a caller that arrives while the bound is saturated is shed with
    ``ResourceExhausted`` instead of entering the engine. Before this bound existed the per-call
    ``maximum_pending_requests`` check was the only limit, and N supervisor threads could each
    admit a full call, putting N x the bound into the model at once.

    The default is one concurrent execution. A deployment that means to run more must pass
    ``limits`` built from its own ``WorkerProcessConfig.maximum_concurrent_executions``; the
    adapter deliberately does not infer a bound from ``ModelWorkerConfig``, which describes one
    call's shape rather than the process's capacity.
    """

    def __init__(
        self,
        config: ModelWorkerConfig,
        planner: BatchPlanner,
        engine: ModelEngine,
        lifecycle: Lifecycle | None = None,
        *,
        limits: WorkerLimits | None = None,
    ) -> None:
        with _contract_failures_as(
            InvalidArgument,
            "model worker configuration is invalid",
            reason="model_worker_config",
        ):
            config.validate()
        resolved_limits = limits or WorkerLimits()
        if not isinstance(resolved_limits, WorkerLimits):
            raise InvalidArgument("model worker limits are invalid", reason="worker_limits")
        self._config = config
        self._planner = planner
        self._engine = engine
        self._limits = resolved_limits
        self._lifecycle = lifecycle or Lifecycle()
        self._capacity = Semaphore(resolved_limits.maximum_concurrent_executions)
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
        if not requests or len(requests) > self._config.maximum_pending_requests:
            raise InvalidArgument(
                "model-worker request count is outside bounds",
                reason="model_worker_request_count",
            )
        # Capacity is taken before the lifecycle counter so a rejected caller never registers as
        # in-flight work that a drain would then wait on.
        if not self._capacity.acquire(blocking=False):
            raise ResourceExhausted(
                "model worker concurrency capacity is exhausted",
                reason="worker_concurrency_exhausted",
            )
        began = False
        try:
            # begin() refuses anything but READY and counts the execution, which is what makes
            # stop() fail closed while this call is still inside the engine.
            self._lifecycle.begin()
            began = True
            return self._execute_admitted(requests, now_unix_millis=now_unix_millis)
        finally:
            try:
                if began:
                    self._lifecycle.finish()
            finally:
                self._capacity.release()

    def drain_and_stop(self) -> bool:
        """Stop accepting work, wait out the bounded drain, and stop only once quiescent."""

        self._lifecycle.drain()
        drained = self._lifecycle.wait_drained(self._limits.drain_timeout_millis)
        if drained:
            self._lifecycle.stop()
        return drained

    def _execute_admitted(
        self,
        requests: tuple[InferenceRequest, ...],
        *,
        now_unix_millis: int,
    ) -> tuple[InferenceResult, ...]:
        admitted: dict[str, InferenceRequest] = {}
        for request in requests:
            with _contract_failures_as(
                InvalidArgument,
                "model-worker request failed contract validation",
                reason="model_worker_request",
            ):
                request.validate(now_unix_millis)
            admitted[request.request_id] = request
        if len(admitted) != len(requests):
            # Duplicate ids used to survive: the old post-hoc set comparison collapsed them, so
            # a caller could submit the same request twice and receive one result for both.
            raise InvalidArgument(
                "model-worker requests must carry unique request ids",
                reason="model_worker_duplicate_request",
            )

        plans = self._planner.plan(requests)
        seen: set[str] = set()
        results: list[InferenceResult] = []
        for plan in plans:
            with _contract_failures_as(
                FailedPrecondition,
                "planner returned a batch that fails the batch contract",
                reason="model_worker_batch_plan",
            ):
                plan.validate()
            if len(plan.requests) > self._config.maximum_batch_requests:
                raise ResourceExhausted(
                    "tensor batch exceeds request bound",
                    reason="model_worker_batch_requests",
                )
            if plan.estimated_gpu_bytes > self._config.maximum_gpu_bytes_per_batch:
                raise ResourceExhausted(
                    "tensor batch exceeds GPU budget",
                    reason="model_worker_batch_gpu_bytes",
                )
            # Bound the planner *before* the engine runs, and bound the whole request rather than
            # its id alone. BatchPlan.validate checks ids and digests but never re-runs
            # InferenceRequest.validate, so a planner that reused an admitted id while swapping
            # the input descriptor would otherwise hand the model a locator and a lease this
            # call never validated.
            for planned in plan.requests:
                original = admitted.get(planned.request_id)
                if original is None:
                    raise FailedPrecondition(
                        "planner scheduled a request this call did not admit",
                        reason="model_worker_unadmitted_request",
                    )
                if planned != original:
                    raise FailedPrecondition(
                        "planner altered an admitted request",
                        reason="model_worker_altered_request",
                    )
                if planned.request_id in seen:
                    raise FailedPrecondition(
                        "planner scheduled a request more than once",
                        reason="model_worker_duplicate_scheduling",
                    )
                seen.add(planned.request_id)
            batch_results = self._engine.execute(plan)
            for result in batch_results:
                with _contract_failures_as(
                    FailedPrecondition,
                    "engine returned a result that fails the result contract",
                    reason="model_worker_result",
                ):
                    result.validate()
                results.append(result)
            # Attribution is checked per batch, not only per call: an engine that answered batch
            # A with batch B's results still produces the right set overall, while handing each
            # caller an output computed from a different request's inputs.
            if sorted(result.request_id for result in batch_results) != sorted(
                planned.request_id for planned in plan.requests
            ):
                raise FailedPrecondition(
                    "engine did not answer exactly the batch it was given",
                    reason="model_worker_batch_attribution",
                )

        produced = {result.request_id for result in results}
        # len(results) is checked as well as the identity set: a set comparison alone lets an
        # engine return the same request_id twice and still look complete.
        if seen != set(admitted) or produced != set(admitted) or len(results) != len(requests):
            raise FailedPrecondition(
                "planner/engine did not produce exactly one result per request",
                reason="model_worker_result_coverage",
            )
        return tuple(results)
