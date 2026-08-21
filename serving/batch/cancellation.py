# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded cooperative cancellation registry for batch jobs."""

from __future__ import annotations

from threading import Lock

from libs.python.worker_runtime import CancellationToken


class CancellationRegistry:
    def __init__(self, maximum_jobs: int) -> None:
        if isinstance(maximum_jobs, bool) or not 1 <= maximum_jobs <= 100_001:
            raise ValueError("cancellation registry limit is outside bounds")
        self._maximum_jobs = maximum_jobs
        self._tokens: dict[str, CancellationToken] = {}
        self._lock = Lock()

    def register(self, job_id: str) -> CancellationToken:
        with self._lock:
            if job_id in self._tokens:
                raise ValueError("batch job is already registered")
            if len(self._tokens) >= self._maximum_jobs:
                raise RuntimeError("batch cancellation registry is full")
            token = CancellationToken()
            self._tokens[job_id] = token
            return token

    def cancel(self, job_id: str) -> bool:
        with self._lock:
            token = self._tokens.get(job_id)
        if token is None:
            return False
        token.cancel()
        return True

    def token(self, job_id: str) -> CancellationToken:
        with self._lock:
            token = self._tokens.get(job_id)
        if token is None:
            raise RuntimeError("batch job has no cancellation token")
        return token

    def release(self, job_id: str) -> None:
        with self._lock:
            self._tokens.pop(job_id, None)
