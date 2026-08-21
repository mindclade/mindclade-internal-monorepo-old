# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded concurrent load harness with exact accounting."""

from __future__ import annotations

import math
from collections.abc import Callable
from concurrent.futures import FIRST_COMPLETED, Future, ThreadPoolExecutor, wait
from dataclasses import dataclass
from time import perf_counter


@dataclass(frozen=True, slots=True)
class LoadResult:
    operations: int
    succeeded: int
    failed: int
    elapsed_seconds: float
    latency_seconds: tuple[float, ...]

    @property
    def throughput_per_second(self) -> float:
        return self.succeeded / self.elapsed_seconds if self.elapsed_seconds > 0 else 0.0

    def percentile(self, percentile: float) -> float:
        if not math.isfinite(percentile) or not 0 <= percentile <= 100:
            raise ValueError("percentile is outside bounds")
        if not self.latency_seconds:
            return 0.0
        ordered = sorted(self.latency_seconds)
        index = math.ceil(percentile / 100 * len(ordered)) - 1
        return ordered[max(0, index)]


def run_load(
    operation: Callable[[int], object], *, operations: int, concurrency: int
) -> LoadResult:
    if not callable(operation):
        raise ValueError("load operation must be callable")
    if isinstance(operations, bool) or not 1 <= operations <= 1_000_000:
        raise ValueError("load operation count is outside bounds")
    if isinstance(concurrency, bool) or not 1 <= concurrency <= min(operations, 4096):
        raise ValueError("load concurrency is outside bounds")
    started = perf_counter()
    latencies: list[float] = []
    succeeded = 0
    failed = 0

    def invoke(index: int) -> float:
        operation_started = perf_counter()
        operation(index)
        return perf_counter() - operation_started

    with ThreadPoolExecutor(max_workers=concurrency, thread_name_prefix="serving-load") as pool:
        next_index = 0
        pending: set[Future[float]] = set()

        def submit_next() -> bool:
            nonlocal next_index
            if next_index >= operations:
                return False
            pending.add(pool.submit(invoke, next_index))
            next_index += 1
            return True

        for _ in range(concurrency):
            submit_next()
        while pending:
            completed, outstanding = wait(pending, return_when=FIRST_COMPLETED)
            pending = set(outstanding)
            for future in completed:
                try:
                    latencies.append(future.result())
                    succeeded += 1
                except Exception:
                    failed += 1
                submit_next()
    elapsed = perf_counter() - started
    return LoadResult(operations, succeeded, failed, elapsed, tuple(latencies))
