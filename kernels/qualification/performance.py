# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass


@dataclass(frozen=True, slots=True)
class PerformanceEvidence:
    samples: int
    warmup: int
    candidate_median_ms: float
    baseline_median_ms: float
    candidate_p90_ms: float
    relative_mad: float
    compile_ms: float

    def __post_init__(self) -> None:
        if self.samples < 5 or self.warmup <= 0:
            raise ValueError("performance evidence requires warmup and at least five samples")
        values = (
            self.candidate_median_ms,
            self.baseline_median_ms,
            self.candidate_p90_ms,
            self.compile_ms,
        )
        if any(value <= 0 for value in values) or not 0 <= self.relative_mad <= 1:
            raise ValueError("performance timings must be positive and dispersion bounded")

    @property
    def speedup(self) -> float:
        return self.baseline_median_ms / self.candidate_median_ms

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()
