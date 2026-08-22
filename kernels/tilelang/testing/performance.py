# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass

from kernels.tilelang.autotune.objective import LatencyDistribution


@dataclass(frozen=True, slots=True)
class BenchmarkResult:
    operation: str
    request_digest: str
    implementation_digest: str
    environment_digest: str
    warmup: int
    latency: LatencyDistribution
    correctness_passed: bool

    def __post_init__(self) -> None:
        if self.warmup <= 0:
            raise ValueError("benchmarks require warmup")
        if not self.correctness_passed:
            raise ValueError("performance results cannot be recorded before correctness passes")


def benchmark_callable(
    function: Callable[[], object],
    *,
    operation: str,
    request_digest: str,
    implementation_digest: str,
    environment_digest: str,
    synchronize: Callable[[], None],
    correctness_passed: bool,
    warmup: int = 10,
    repeats: int = 50,
) -> BenchmarkResult:
    """Measure an already-correct callable with explicit device synchronization.

    The caller supplies the backend synchronization primitive (for example,
    ``torch.cuda.synchronize``), which prevents asynchronous launch latency from
    being mistaken for device execution time. Results use median and MAD and do
    not imply a speedup or production qualification.
    """

    if not correctness_passed:
        raise ValueError("benchmarking is forbidden until correctness passes")
    if warmup <= 0 or repeats < 5:
        raise ValueError("benchmarking requires warmup > 0 and at least five repeats")

    for _ in range(warmup):
        function()
    synchronize()

    samples_ms: list[float] = []
    for _ in range(repeats):
        synchronize()
        started = time.perf_counter_ns()
        function()
        synchronize()
        samples_ms.append((time.perf_counter_ns() - started) / 1_000_000.0)

    latency = LatencyDistribution(samples_ms=tuple(samples_ms))
    return BenchmarkResult(
        operation=operation,
        request_digest=request_digest,
        implementation_digest=implementation_digest,
        environment_digest=environment_digest,
        warmup=warmup,
        latency=latency,
        correctness_passed=True,
    )
