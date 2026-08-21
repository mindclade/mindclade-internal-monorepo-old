# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import math


def expert_capacity(
    tokens: int,
    experts: int,
    top_k: int,
    *,
    capacity_factor: float = 1.25,
    minimum: int = 1,
) -> int:
    if tokens <= 0 or experts <= 0 or top_k <= 0 or top_k > experts:
        raise ValueError("tokens, experts, and top_k must define a valid positive routing shape")
    if capacity_factor < 1.0 or minimum <= 0:
        raise ValueError("capacity_factor must be at least one and minimum must be positive")
    return max(minimum, math.ceil(tokens * top_k * capacity_factor / experts))
