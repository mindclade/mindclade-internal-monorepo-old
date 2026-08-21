# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Aggregate categorical drift metrics."""

from __future__ import annotations

import math
from collections.abc import Mapping


def total_variation(reference: Mapping[str, float], candidate: Mapping[str, float]) -> float:
    if not reference or not candidate or set(reference) != set(candidate):
        raise ValueError("drift distributions require identical non-empty domains")
    for distribution in (reference, candidate):
        if any(
            isinstance(value, bool) or not math.isfinite(value) or value < 0
            for value in distribution.values()
        ) or not math.isclose(sum(distribution.values()), 1.0, abs_tol=1e-9):
            raise ValueError("drift distributions must be normalized probabilities")
    return 0.5 * sum(abs(reference[key] - candidate[key]) for key in reference)
