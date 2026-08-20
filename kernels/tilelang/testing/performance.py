# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

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
