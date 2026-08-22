# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import statistics
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class LatencyDistribution:
    samples_ms: tuple[float, ...]

    def __post_init__(self) -> None:
        if len(self.samples_ms) < 5 or any(value <= 0 for value in self.samples_ms):
            raise ValueError("latency distributions need at least five positive samples")

    @property
    def median_ms(self) -> float:
        return statistics.median(self.samples_ms)

    @property
    def p90_ms(self) -> float:
        ordered = sorted(self.samples_ms)
        return ordered[max(0, int(0.9 * len(ordered)) - 1)]

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
