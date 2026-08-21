# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded earliest-deadline-first queue with stable FIFO tie breaking."""

from __future__ import annotations

import heapq
from threading import Lock

from .job import BatchJob


class JobQueue:
    def __init__(self, capacity: int) -> None:
        if isinstance(capacity, bool) or not 1 <= capacity <= 100_000:
            raise ValueError("batch queue capacity is outside bounds")
        self._capacity = capacity
        self._sequence = 0
        self._jobs: list[tuple[int, int, BatchJob]] = []
        self._identifiers: set[str] = set()
        self._lock = Lock()

    def put(self, job: BatchJob) -> None:
        with self._lock:
            if job.job_id in self._identifiers:
                raise ValueError("batch job id is already queued")
            if len(self._jobs) >= self._capacity:
                raise RuntimeError("batch job queue is full")
            self._sequence += 1
            heapq.heappush(self._jobs, (job.deadline_unix_millis, self._sequence, job))
            self._identifiers.add(job.job_id)

    def get_nowait(self) -> BatchJob | None:
        with self._lock:
            if not self._jobs:
                return None
            _, _, job = heapq.heappop(self._jobs)
            self._identifiers.remove(job.job_id)
            return job

    def __len__(self) -> int:
        with self._lock:
            return len(self._jobs)
