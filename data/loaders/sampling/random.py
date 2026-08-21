# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Local deterministic random sampling without global RNG state."""

from __future__ import annotations

import random


def sample_indices(population: int, count: int, *, seed: int) -> tuple[int, ...]:
    if any(
        isinstance(value, bool) or not isinstance(value, int) or value < 0
        for value in (population, count, seed)
    ) or count > population:
        raise ValueError("random sampling bounds are invalid")
    return tuple(random.Random(seed).sample(range(population), count))
