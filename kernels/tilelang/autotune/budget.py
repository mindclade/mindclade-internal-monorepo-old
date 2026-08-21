# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class TuningBudget:
    max_candidates: int = 32
    compile_timeout_seconds: float = 120.0
    candidate_timeout_seconds: float = 30.0
    warmup_samples: int = 10
    benchmark_samples: int = 50

    def __post_init__(self) -> None:
        if not 1 <= self.max_candidates <= 256:
            raise ValueError("max_candidates must be between one and 256")
        if self.compile_timeout_seconds <= 0 or self.candidate_timeout_seconds <= 0:
            raise ValueError("timeouts must be positive")
        if self.warmup_samples < 1 or self.benchmark_samples < 5:
            raise ValueError("at least one warmup and five benchmark samples are required")
