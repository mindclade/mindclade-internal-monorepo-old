# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable construction of right-handed coordinate frames."""

from __future__ import annotations

import numpy as np

from libs.python.errors import InvalidArgument

from .invariants import as_vector3
from .rigid import RigidTransform


def frame_from_axes(origin: object, x_axis: object, xy_direction: object) -> RigidTransform:
    """Construct a frame from an origin, x-axis direction, and xy-plane direction."""
    translation = as_vector3(origin, name="origin")
    x = as_vector3(x_axis, name="x_axis")
    xy = as_vector3(xy_direction, name="xy_direction")
    x_norm = float(np.linalg.norm(x))
    if x_norm <= np.finfo(np.float64).eps:
        raise InvalidArgument("x_axis must be non-zero", reason="geometry_degenerate_axis")
    x_unit = x / x_norm
    z = np.cross(x_unit, xy)
    z_norm = float(np.linalg.norm(z))
    if z_norm <= np.finfo(np.float64).eps:
        raise InvalidArgument(
            "xy_direction must not be parallel to x_axis",
            reason="geometry_degenerate_axis",
        )
    z_unit = z / z_norm
    y_unit = np.cross(z_unit, x_unit)
    rotation = np.column_stack((x_unit, y_unit, z_unit))
    return RigidTransform(rotation, translation)
