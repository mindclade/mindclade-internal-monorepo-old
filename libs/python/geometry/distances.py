# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded Euclidean distance and deviation calculations."""

from __future__ import annotations

import math
from typing import Final

import numpy as np

from libs.python.errors import InvalidArgument, ResourceExhausted

from .invariants import FloatArray, as_points, readonly_copy

MAXIMUM_PAIRWISE_ELEMENTS: Final = 4_000_000


def pairwise_distances(first: object, second: object | None = None) -> FloatArray:
    left = as_points(first, name="first points").reshape(-1, 3)
    right = left if second is None else as_points(second, name="second points").reshape(-1, 3)
    element_count = len(left) * len(right)
    if element_count > MAXIMUM_PAIRWISE_ELEMENTS:
        raise ResourceExhausted(
            f"pairwise distance matrix exceeds {MAXIMUM_PAIRWISE_ELEMENTS} elements",
            reason="geometry_pairwise_size",
        )
    difference = left[:, None, :] - right[None, :, :]
    return readonly_copy(np.linalg.norm(difference, axis=-1))


def root_mean_square_deviation(first: object, second: object) -> float:
    left = as_points(first, name="first points")
    right = as_points(second, name="second points")
    if left.shape != right.shape or left.size == 0:
        raise InvalidArgument(
            "RMSD inputs must have the same non-empty shape",
            reason="geometry_rmsd_shape",
        )
    squared = np.square(left - right)
    return math.sqrt(float(np.mean(np.sum(squared, axis=-1))))
