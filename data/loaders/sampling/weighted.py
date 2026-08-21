# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Seeded weighted index selection with replacement."""

from __future__ import annotations

import math
import random
from collections.abc import Iterable


def weighted_indices(weights: Iterable[float], count: int, *, seed: int) -> tuple[int, ...]:
    values = tuple(weights)
    if (
        not values
        or isinstance(count, bool)
        or not isinstance(count, int)
        or count < 0
        or isinstance(seed, bool)
        or not isinstance(seed, int)
        or seed < 0
        or any(isinstance(value, bool) or not math.isfinite(value) or value <= 0 for value in values)
    ):
        raise ValueError("weighted sampling inputs are invalid")
    return tuple(random.Random(seed).choices(range(len(values)), weights=values, k=count))
