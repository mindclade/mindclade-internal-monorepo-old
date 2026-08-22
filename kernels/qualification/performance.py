# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
import math
from dataclasses import asdict, dataclass
from typing import Any


@dataclass(frozen=True, slots=True)
class PerformanceEvidence:
    samples: int
    process_repeats: int
    warmup: int
    candidate_median_ms: float
    baseline_median_ms: float
    candidate_p90_ms: float
    candidate_p95_ms: float
    baseline_p95_ms: float
    relative_mad: float
    compile_ms: float
    candidate_peak_memory_bytes: int
    baseline_peak_memory_bytes: int
    raw_results_digest: str

    def __post_init__(self) -> None:
        counts = (self.samples, self.process_repeats, self.warmup)
        if any(isinstance(value, bool) or not isinstance(value, int) for value in counts):
            raise TypeError("performance sample, repeat, and warmup counts must be integers")
        if self.samples < 5 or self.process_repeats <= 0 or self.warmup <= 0:
            raise ValueError("performance evidence requires warmup, repeats, and five samples")
        values = (
            self.candidate_median_ms,
            self.baseline_median_ms,
            self.candidate_p90_ms,
            self.candidate_p95_ms,
            self.baseline_p95_ms,
            self.compile_ms,
        )
        if (
            any(
                isinstance(value, bool)
                or not isinstance(value, int | float)
                or not math.isfinite(value)
                or value <= 0
                for value in values
            )
            or not isinstance(self.relative_mad, int | float)
            or isinstance(self.relative_mad, bool)
            or not math.isfinite(self.relative_mad)
            or not 0 <= self.relative_mad <= 1
        ):
            raise ValueError("performance timings must be positive and dispersion bounded")
        if self.candidate_p90_ms < self.candidate_median_ms:
            raise ValueError("candidate p90 must not be below its median")
        if self.candidate_p95_ms < self.candidate_p90_ms:
            raise ValueError("candidate p95 must not be below p90")
        if self.baseline_p95_ms < self.baseline_median_ms:
            raise ValueError("baseline p95 must not be below its median")
        memory_values = (
            self.candidate_peak_memory_bytes,
            self.baseline_peak_memory_bytes,
        )
        if any(
            isinstance(value, bool) or not isinstance(value, int) or value <= 0
            for value in memory_values
        ):
            raise ValueError("peak device memory measurements must be positive")
        if len(self.raw_results_digest) != 64 or any(
            character not in "0123456789abcdef" for character in self.raw_results_digest
        ):
            raise ValueError("raw_results_digest must be a lowercase SHA-256 digest")

    @property
    def speedup(self) -> float:
        return self.baseline_median_ms / self.candidate_median_ms

    @property
    def p95_ratio(self) -> float:
        return self.candidate_p95_ms / self.baseline_p95_ms

    @property
    def memory_ratio(self) -> float:
        return self.candidate_peak_memory_bytes / self.baseline_peak_memory_bytes

    def canonical(self) -> dict[str, object]:
        return asdict(self)

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> PerformanceEvidence:
        return cls(
            samples=payload["samples"],
            process_repeats=payload["process_repeats"],
            warmup=payload["warmup"],
            candidate_median_ms=payload["candidate_median_ms"],
            baseline_median_ms=payload["baseline_median_ms"],
            candidate_p90_ms=payload["candidate_p90_ms"],
            candidate_p95_ms=payload["candidate_p95_ms"],
            baseline_p95_ms=payload["baseline_p95_ms"],
            relative_mad=payload["relative_mad"],
            compile_ms=payload["compile_ms"],
            candidate_peak_memory_bytes=payload["candidate_peak_memory_bytes"],
            baseline_peak_memory_bytes=payload["baseline_peak_memory_bytes"],
            raw_results_digest=payload["raw_results_digest"],
        )

    @property
    def digest(self) -> str:
        payload = json.dumps(self.canonical(), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()
