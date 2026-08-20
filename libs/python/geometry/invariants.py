# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shape, finiteness, and rigid-geometry invariants."""

from __future__ import annotations

from typing import Final

import numpy as np
import numpy.typing as npt

from libs.python.errors import InvalidArgument, ResourceExhausted

FloatArray = npt.NDArray[np.float64]
MAXIMUM_POINTS: Final = 1_000_000
DEFAULT_ORTHONORMAL_ATOL: Final = 1e-7


def _float64(value: object, *, name: str) -> FloatArray:
    try:
        array = np.asarray(value, dtype=np.float64)
    except (TypeError, ValueError) as error:
        raise InvalidArgument(
            f"{name} must be numeric",
            reason="geometry_numeric",
            cause=error,
        ) from error
    if not np.all(np.isfinite(array)):
        raise InvalidArgument(
            f"{name} must contain only finite values",
            reason="geometry_finite",
        )
    return array


def readonly_copy(value: FloatArray) -> FloatArray:
    copied = np.array(value, dtype=np.float64, copy=True)
    copied.flags.writeable = False
    return copied


def as_vector3(value: object, *, name: str = "vector") -> FloatArray:
    array = _float64(value, name=name)
    if array.shape != (3,):
        raise InvalidArgument(
            f"{name} must have shape (3,), got {array.shape}",
            reason="geometry_vector_shape",
        )
    return readonly_copy(array)


def as_points(value: object, *, name: str = "points") -> FloatArray:
    array = _float64(value, name=name)
    if array.ndim < 1 or array.shape[-1] != 3:
        raise InvalidArgument(
            f"{name} must have trailing dimension 3, got {array.shape}",
            reason="geometry_points_shape",
        )
    point_count = int(array.size // 3)
    if point_count > MAXIMUM_POINTS:
        raise ResourceExhausted(
            f"{name} exceeds the {MAXIMUM_POINTS}-point bound",
            reason="geometry_point_count",
        )
    return readonly_copy(array)


def as_rotation_matrix(value: object, *, atol: float = DEFAULT_ORTHONORMAL_ATOL) -> FloatArray:
    if not isinstance(atol, int | float) or isinstance(atol, bool) or not 0 < atol <= 1e-2:
        raise InvalidArgument(
            "rotation tolerance must be in (0, 1e-2]",
            reason="geometry_tolerance",
        )
    array = _float64(value, name="rotation")
    if array.shape != (3, 3):
        raise InvalidArgument(
            f"rotation must have shape (3, 3), got {array.shape}",
            reason="geometry_rotation_shape",
        )
    if not np.allclose(array.T @ array, np.eye(3), rtol=0.0, atol=atol):
        raise InvalidArgument(
            "rotation must be orthonormal",
            reason="geometry_rotation_orthonormal",
        )
    determinant = float(np.linalg.det(array))
    if not np.isclose(determinant, 1.0, rtol=0.0, atol=atol):
        raise InvalidArgument(
            "rotation must be right-handed with determinant +1",
            reason="geometry_rotation_handedness",
        )
    return readonly_copy(array)
