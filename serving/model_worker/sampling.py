# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated statistical sampling controls with explicit seed provenance."""

import math
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SamplingParameters:
    seed: int
    temperature: float = 1.0
    top_p: float = 1.0
    top_k: int = 0

    def __post_init__(self) -> None:
        if isinstance(self.seed, bool) or not 0 <= self.seed < 2**64:
            raise ValueError("sampling seed is invalid")
        if not math.isfinite(self.temperature) or not 0 < self.temperature <= 10:
            raise ValueError("sampling temperature is outside bounds")
        if not math.isfinite(self.top_p) or not 0 < self.top_p <= 1:
            raise ValueError("sampling top_p is outside bounds")
        if isinstance(self.top_k, bool) or not 0 <= self.top_k <= 1_000_000:
            raise ValueError("sampling top_k is outside bounds")
