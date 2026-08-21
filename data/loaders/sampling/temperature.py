# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Temperature scaling for positive dataset weights."""

from __future__ import annotations

import math
from collections.abc import Iterable


def temperature_weights(weights: Iterable[float], temperature: float) -> tuple[float, ...]:
    values = tuple(weights)
    if (
        not values
        or isinstance(temperature, bool)
        or not math.isfinite(temperature)
        or temperature <= 0
        or any(isinstance(value, bool) or not math.isfinite(value) or value <= 0 for value in values)
    ):
        raise ValueError("temperature weights require finite positive values")
    scaled = tuple(value ** (1.0 / temperature) for value in values)
    total = sum(scaled)
    return tuple(value / total for value in scaled)
