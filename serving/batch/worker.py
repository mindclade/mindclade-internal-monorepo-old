# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Synchronous composition root used beneath the durable Rust/Go control path."""

from __future__ import annotations

from time import time_ns

from .cancellation import CancellationRegistry
from .config import BatchLimits
from .executor import BatchExecutor
from .health import BatchHealth
from .job import BatchJob
from .queue import JobQueue
from .result import BatchResult
from .telemetry import BatchTelemetry


class BatchWorker:
    def __init__(self, limits: BatchLimits, executor: BatchExecutor) -> None:
        self._limits = limits
        self._executor = executor
        self._queue = JobQueue(limits.maximum_queued_jobs)
        self._cancellations = CancellationRegistry(limits.maximum_queued_jobs + 1)
        self._telemetry = BatchTelemetry()
        self._ready = False
        self._draining = False

    def ready(self) -> None:
        if self._draining:
            raise RuntimeError("batch worker is draining")
        self._ready = True

    def submit(self, job: BatchJob, *, now_unix_millis: int | None = None) -> None:
        if not self._ready or self._draining:
            raise RuntimeError("batch worker is not accepting jobs")
        now = time_ns() // 1_000_000 if now_unix_millis is None else now_unix_millis
        job.validate(self._limits, now_unix_millis=now)
        self._cancellations.register(job.job_id)
        try:
            self._queue.put(job)
        except BaseException:
            self._cancellations.release(job.job_id)
            raise
        self._telemetry.increment("admitted")

    def run_next(self, *, now_unix_millis: int | None = None) -> BatchResult | None:
        job = self._queue.get_nowait()
        if job is None:
            return None
        token = self._cancellations.token(job.job_id)
        now = time_ns() // 1_000_000 if now_unix_millis is None else now_unix_millis
        try:
            result = self._executor.execute(job, now_unix_millis=now, cancellation=token)
            self._telemetry.increment("completed")
            return result
        except BaseException:
            self._telemetry.increment("canceled" if token.is_cancelled else "failed")
            raise
        finally:
            self._cancellations.release(job.job_id)

    def cancel(self, job_id: str) -> bool:
        return self._cancellations.cancel(job_id)

    def drain(self) -> None:
        self._draining = True
        self._ready = False

    def health(self) -> BatchHealth:
        return BatchHealth(
            self._ready, self._draining, len(self._queue), self._limits.maximum_queued_jobs
        )

    @property
    def telemetry(self) -> BatchTelemetry:
        return self._telemetry
