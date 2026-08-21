# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Aggregate representation disparity without inventing protected labels."""

from __future__ import annotations

from collections.abc import Mapping


def max_representation_ratio(counts: Mapping[str, int]) -> float:
    """Return max/min non-zero representation; policy chooses acceptance thresholds."""

    if not counts or any(
        isinstance(value, bool) or not isinstance(value, int) or value < 0
        for value in counts.values()
    ):
        raise ValueError("bias counts must be non-negative integers")
    nonzero = [value for value in counts.values() if value]
    if not nonzero:
        raise ValueError("bias counts require at least one represented group")
    return max(nonzero) / min(nonzero)
