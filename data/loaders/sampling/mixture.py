# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Dataset-mixture component schedule."""

from __future__ import annotations

from .weighted import weighted_indices


def mixture_schedule(weights: tuple[float, ...], steps: int, *, seed: int) -> tuple[int, ...]:
    return weighted_indices(weights, steps, seed=seed)
