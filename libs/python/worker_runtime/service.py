# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded process-local lifecycle for Python scientific workers.

Rust remains the security and process boundary: it verifies execution tickets,
fencing, artifact grants, and resource budgets before calling this package.  The
classes here prevent a Python adapter from accepting work while starting or
draining, cap concurrent numerical executions, and make shutdown observable.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from threading import Condition, Lock, Semaphore
from time import monotonic

from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted

from .contracts import StageResult
from .executor import CancellationToken, StageExecutor
from .workload import WorkloadEnvelope

MAXIMUM_CONCURRENT_EXECUTIONS = 1024
MAXIMUM_DRAIN_MILLIS = 300_000


@dataclass(frozen=True, slots=True)
class WorkerLimits:
    """Local defense-in-depth limits beneath the signed Rust budget."""

    maximum_concurrent_executions: int = 1
    drain_timeout_millis: int = 30_000

    def __post_init__(self) -> None:
        if (
            isinstance(self.maximum_concurrent_executions, bool)
            or not isinstance(self.maximum_concurrent_executions, int)
            or not 1 <= self.maximum_concurrent_executions <= MAXIMUM_CONCURRENT_EXECUTIONS
        ):
            raise InvalidArgument(
                "maximum_concurrent_executions is outside bounds",
                reason="worker_concurrency_limit",
            )
        if (
            isinstance(self.drain_timeout_millis, bool)
            or not isinstance(self.drain_timeout_millis, int)
            or not 1 <= self.drain_timeout_millis <= MAXIMUM_DRAIN_MILLIS
        ):
            raise InvalidArgument(
                "drain_timeout_millis is outside bounds",
                reason="worker_drain_limit",
            )


class WorkerState(StrEnum):
    STARTING = "starting"
    READY = "ready"
    DRAINING = "draining"
    STOPPED = "stopped"


class WorkerLifecycle:
    """Thread-safe lifecycle with an exact in-flight execution count."""

    def __init__(self) -> None:
        self._state = WorkerState.STARTING
        self._active = 0
        self._condition = Condition(Lock())

    @property
    def state(self) -> WorkerState:
        with self._condition:
            return self._state

    @property
    def active_executions(self) -> int:
        with self._condition:
            return self._active

    @property
    def accepting(self) -> bool:
        return self.state is WorkerState.READY

    def ready(self) -> None:
        with self._condition:
            if self._state is not WorkerState.STARTING:
                raise FailedPrecondition(
                    "worker can become ready only while starting",
                    reason="worker_lifecycle_transition",
                )
            self._state = WorkerState.READY

    def begin(self) -> None:
        with self._condition:
            if self._state is not WorkerState.READY:
                raise FailedPrecondition(
                    "worker is not accepting execution",
                    reason="worker_not_ready",
                    fields={"state": self._state.value},
                )
            self._active += 1

    def finish(self) -> None:
        with self._condition:
            if self._active == 0:
                raise FailedPrecondition(
                    "worker execution counter is already empty",
                    reason="worker_execution_counter",
                )
            self._active -= 1
            if self._active == 0:
                self._condition.notify_all()

    def drain(self) -> None:
        with self._condition:
            if self._state is WorkerState.READY:
                self._state = WorkerState.DRAINING
                if self._active == 0:
                    self._condition.notify_all()
                return
            if self._state is not WorkerState.DRAINING:
                raise FailedPrecondition(
                    "worker can drain only while ready",
                    reason="worker_lifecycle_transition",
                    fields={"state": self._state.value},
                )

    def wait_drained(self, timeout_millis: int) -> bool:
        if isinstance(timeout_millis, bool) or not isinstance(timeout_millis, int):
            raise InvalidArgument("drain timeout must be an integer", reason="worker_drain")
        if not 0 <= timeout_millis <= MAXIMUM_DRAIN_MILLIS:
            raise InvalidArgument("drain timeout is outside bounds", reason="worker_drain")
        deadline = monotonic() + timeout_millis / 1000
        with self._condition:
            while self._active:
                remaining = deadline - monotonic()
                if remaining <= 0:
                    return False
                self._condition.wait(remaining)
            return True

    def stop(self) -> None:
        with self._condition:
            if self._state not in {WorkerState.STARTING, WorkerState.DRAINING}:
                raise FailedPrecondition(
                    "worker can stop only while starting or draining",
                    reason="worker_lifecycle_transition",
                    fields={"state": self._state.value},
                )
            if self._active:
                raise FailedPrecondition(
                    "worker cannot stop with active executions",
                    reason="worker_active_during_stop",
                    fields={"active": str(self._active)},
                )
            self._state = WorkerState.STOPPED


class StageWorker:
    """A thin bounded adapter around an injected scientific stage engine."""

    def __init__(
        self,
        executor: StageExecutor,
        limits: WorkerLimits | None = None,
        lifecycle: WorkerLifecycle | None = None,
    ) -> None:
        if not isinstance(executor, StageExecutor):
            raise InvalidArgument("worker executor is invalid", reason="worker_executor")
        resolved_limits = limits or WorkerLimits()
        if not isinstance(resolved_limits, WorkerLimits):
            raise InvalidArgument("worker limits are invalid", reason="worker_limits")
        self._executor = executor
        self._limits = resolved_limits
        self._lifecycle = lifecycle or WorkerLifecycle()
        self._capacity = Semaphore(resolved_limits.maximum_concurrent_executions)

    @property
    def lifecycle(self) -> WorkerLifecycle:
        return self._lifecycle

    def ready(self) -> None:
        self._lifecycle.ready()

    def execute(
        self,
        workload: WorkloadEnvelope,
        *,
        cancellation: CancellationToken | None = None,
    ) -> StageResult:
        if not isinstance(workload, WorkloadEnvelope):
            raise InvalidArgument("worker workload is invalid", reason="worker_workload")
        workload.validate()
        if not self._capacity.acquire(blocking=False):
            raise ResourceExhausted(
                "worker concurrency capacity is exhausted",
                reason="worker_concurrency_exhausted",
            )
        began = False
        try:
            self._lifecycle.begin()
            began = True
            return self._executor.execute(workload.stage, cancellation=cancellation)
        finally:
            if began:
                self._lifecycle.finish()
            self._capacity.release()

    def drain_and_stop(self) -> bool:
        self._lifecycle.drain()
        drained = self._lifecycle.wait_drained(self._limits.drain_timeout_millis)
        if drained:
            self._lifecycle.stop()
        return drained
