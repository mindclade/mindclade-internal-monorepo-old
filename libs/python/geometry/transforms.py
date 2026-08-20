# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Small, validated constructors and functional rigid-transform operations."""

from __future__ import annotations

import math

import numpy as np

from libs.python.errors import InvalidArgument

from .invariants import FloatArray, as_vector3
from .rigid import RigidTransform


def rotation_about_axis(axis: object, angle_radians: float) -> RigidTransform:
    if (
        isinstance(angle_radians, bool)
        or not isinstance(angle_radians, int | float)
        or not math.isfinite(angle_radians)
    ):
        raise InvalidArgument(
            "rotation angle must be finite",
            reason="geometry_rotation_angle",
        )
    vector = as_vector3(axis, name="axis")
    norm = float(np.linalg.norm(vector))
    if norm <= np.finfo(np.float64).eps:
        raise InvalidArgument("rotation axis must be non-zero", reason="geometry_degenerate_axis")
    x, y, z = vector / norm
    cosine = math.cos(angle_radians)
    sine = math.sin(angle_radians)
    complement = 1.0 - cosine
    rotation = np.array(
        [
            [
                cosine + x * x * complement,
                x * y * complement - z * sine,
                x * z * complement + y * sine,
            ],
            [
                y * x * complement + z * sine,
                cosine + y * y * complement,
                y * z * complement - x * sine,
            ],
            [
                z * x * complement - y * sine,
                z * y * complement + x * sine,
                cosine + z * z * complement,
            ],
        ],
        dtype=np.float64,
    )
    return RigidTransform(rotation, np.zeros(3, dtype=np.float64))


def transform_points(transform: object, points: object) -> FloatArray:
    if not isinstance(transform, RigidTransform):
        raise InvalidArgument(
            "transform_points requires a RigidTransform",
            reason="geometry_transform_type",
        )
    return transform.apply(points)
