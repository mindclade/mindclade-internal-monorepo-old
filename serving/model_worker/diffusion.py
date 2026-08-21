# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated diffusion schedule contract; numerical solver stays model-owned."""

import math
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class DiffusionSchedule:
    timesteps: tuple[float, ...]
    seed: int

    def __post_init__(self) -> None:
        if not 1 <= len(self.timesteps) <= 100_000:
            raise ValueError("diffusion timestep count is outside bounds")
        if any(not math.isfinite(step) or not 0 <= step <= 1 for step in self.timesteps):
            raise ValueError("diffusion timesteps must be finite values in [0, 1]")
        if any(
            left <= right for left, right in zip(self.timesteps, self.timesteps[1:], strict=False)
        ):
            raise ValueError("diffusion timesteps must be strictly descending")
        if isinstance(self.seed, bool) or not 0 <= self.seed < 2**64:
            raise ValueError("diffusion seed is invalid")
