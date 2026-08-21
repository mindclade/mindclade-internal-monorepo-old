# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import math
import statistics
from dataclasses import dataclass

MAXIMUM_LATENCY_SAMPLES = 10_000
MAXIMUM_SAMPLE_MILLISECONDS = 3_600_000.0


@dataclass(frozen=True, slots=True)
class LatencyDistribution:
    samples_ms: tuple[float, ...]

    def __post_init__(self) -> None:
        if not isinstance(self.samples_ms, tuple):
            raise TypeError("latency samples must be a tuple")
        if not 5 <= len(self.samples_ms) <= MAXIMUM_LATENCY_SAMPLES:
            raise ValueError(
                f"latency distributions require between five and {MAXIMUM_LATENCY_SAMPLES} samples"
            )
        for value in self.samples_ms:
            if isinstance(value, bool) or not isinstance(value, (int, float)):
                raise TypeError("latency samples must be real numbers")
            if not math.isfinite(value) or not 0 < value <= MAXIMUM_SAMPLE_MILLISECONDS:
                raise ValueError(
                    "latency samples must be finite, positive, and no greater than "
                    f"{MAXIMUM_SAMPLE_MILLISECONDS} milliseconds"
                )

    @property
    def median_ms(self) -> float:
        return statistics.median(self.samples_ms)

    @property
    def p90_ms(self) -> float:
        ordered = sorted(self.samples_ms)
        return ordered[max(0, int(0.9 * len(ordered)) - 1)]

    @property
    def p95_ms(self) -> float:
        ordered = sorted(self.samples_ms)
        return ordered[max(0, int(0.95 * len(ordered)) - 1)]

    @property
    def relative_mad(self) -> float:
        median = self.median_ms
        return statistics.median(abs(value - median) for value in self.samples_ms) / median


def stable_winner(
    measurements: dict[str, LatencyDistribution], *, max_relative_mad: float = 0.10
) -> str:
    stable = {
        key: value for key, value in measurements.items() if value.relative_mad <= max_relative_mad
    }
    if not stable:
        raise ValueError("no latency distribution meets the stability threshold")
    return min(stable, key=lambda key: (stable[key].median_ms, stable[key].p90_ms, key))
