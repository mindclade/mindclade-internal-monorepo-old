# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Priority weights for replay sampling."""

from __future__ import annotations

import math


def priority_weights(priorities: tuple[float, ...], *, exponent: float) -> tuple[float, ...]:
    if (
        not priorities
        or isinstance(exponent, bool)
        or not math.isfinite(exponent)
        or exponent < 0
        or any(isinstance(value, bool) or not math.isfinite(value) or value <= 0 for value in priorities)
    ):
        raise ValueError("replay priorities/exponent are invalid")
    scaled = tuple(value**exponent for value in priorities)
    total = sum(scaled)
    return tuple(value / total for value in scaled)
