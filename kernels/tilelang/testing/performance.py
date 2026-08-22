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
        if not isinstance(self.operation, str) or not self.operation.strip():
            raise ValueError("benchmark operation must be non-empty")
        for name, digest in (
            ("request_digest", self.request_digest),
            ("implementation_digest", self.implementation_digest),
            ("environment_digest", self.environment_digest),
        ):
            _require_digest(name, digest)
        if isinstance(self.warmup, bool) or not isinstance(self.warmup, int) or self.warmup <= 0:
            raise ValueError("benchmarks require warmup")
        if not isinstance(self.latency, LatencyDistribution):
            raise TypeError("benchmark latency must be a LatencyDistribution")
        if not isinstance(self.correctness_passed, bool) or not self.correctness_passed:
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

    if not callable(function) or not callable(synchronize):
        raise TypeError("benchmark function and synchronizer must be callable")
    if not isinstance(correctness_passed, bool) or not correctness_passed:
        raise ValueError("benchmarking is forbidden until correctness passes")
    if (
        isinstance(warmup, bool)
        or not isinstance(warmup, int)
        or isinstance(repeats, bool)
        or not isinstance(repeats, int)
        or warmup <= 0
        or repeats < 5
    ):
        raise ValueError("benchmarking requires warmup > 0 and at least five repeats")
    if not isinstance(operation, str) or not operation.strip():
        raise ValueError("benchmark operation must be non-empty")
    for name, digest in (
        ("request_digest", request_digest),
        ("implementation_digest", implementation_digest),
        ("environment_digest", environment_digest),
    ):
        _require_digest(name, digest)

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


def _require_digest(name: str, value: object) -> None:
    if (
        not isinstance(value, str)
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ValueError(f"{name} must be a lowercase SHA-256 digest")
