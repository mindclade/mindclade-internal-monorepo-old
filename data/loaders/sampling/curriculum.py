# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Policy-explicit curriculum eligibility selection."""

from __future__ import annotations

import math
from collections.abc import Iterable


def eligible_indices(scores: Iterable[float], *, maximum: float) -> tuple[int, ...]:
    values = tuple(scores)
    if (
        isinstance(maximum, bool)
        or not math.isfinite(maximum)
        or any(isinstance(score, bool) or not math.isfinite(score) for score in values)
    ):
        raise ValueError("curriculum scores and threshold must be finite")
    return tuple(index for index, score in enumerate(values) if score <= maximum)
