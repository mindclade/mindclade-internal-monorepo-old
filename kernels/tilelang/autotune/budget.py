# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import math
from dataclasses import dataclass

MAXIMUM_CANDIDATES = 256
MAXIMUM_TIMEOUT_SECONDS = 3_600.0
MAXIMUM_WARMUP_SAMPLES = 10_000
MAXIMUM_BENCHMARK_SAMPLES = 10_000


@dataclass(frozen=True, slots=True)
class TuningBudget:
    max_candidates: int = 32
    compile_timeout_seconds: float = 120.0
    candidate_timeout_seconds: float = 30.0
    warmup_samples: int = 10
    benchmark_samples: int = 50

    def __post_init__(self) -> None:
        for name, value, lower, upper in (
            ("max_candidates", self.max_candidates, 1, MAXIMUM_CANDIDATES),
            ("warmup_samples", self.warmup_samples, 1, MAXIMUM_WARMUP_SAMPLES),
            ("benchmark_samples", self.benchmark_samples, 5, MAXIMUM_BENCHMARK_SAMPLES),
        ):
            if isinstance(value, bool) or not isinstance(value, int):
                raise TypeError(f"{name} must be an integer")
            if not lower <= value <= upper:
                raise ValueError(f"{name} must be between {lower} and {upper}")

        for name, timeout_value in (
            ("compile_timeout_seconds", self.compile_timeout_seconds),
            ("candidate_timeout_seconds", self.candidate_timeout_seconds),
        ):
            if isinstance(timeout_value, bool) or not isinstance(timeout_value, (int, float)):
                raise TypeError(f"{name} must be a real number")
            if not math.isfinite(timeout_value) or not 0 < timeout_value <= MAXIMUM_TIMEOUT_SECONDS:
                raise ValueError(
                    f"{name} must be finite and between zero and {MAXIMUM_TIMEOUT_SECONDS} seconds"
                )
