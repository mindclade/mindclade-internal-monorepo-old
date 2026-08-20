# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated NumPy rigid geometry and bounded distance calculations."""

from .distances import MAXIMUM_PAIRWISE_ELEMENTS, pairwise_distances, root_mean_square_deviation
from .frames import frame_from_axes
from .invariants import DEFAULT_ORTHONORMAL_ATOL, MAXIMUM_POINTS
from .rigid import RigidTransform
from .transforms import rotation_about_axis, transform_points

__all__ = [
    "DEFAULT_ORTHONORMAL_ATOL",
    "MAXIMUM_PAIRWISE_ELEMENTS",
    "MAXIMUM_POINTS",
    "RigidTransform",
    "frame_from_axes",
    "pairwise_distances",
    "root_mean_square_deviation",
    "rotation_about_axis",
    "transform_points",
]
