# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded numerical assertions with actionable failure diagnostics."""

from __future__ import annotations

import math
from typing import Final

import numpy as np

from libs.python.errors import InvalidArgument, ResourceExhausted

MAXIMUM_ASSERTION_ELEMENTS: Final = 4_000_000


def _array(value: object, *, name: str) -> np.ndarray[tuple[int, ...], np.dtype[np.float64]]:
    try:
        array = np.asarray(value, dtype=np.float64)
    except (TypeError, ValueError) as error:
        raise InvalidArgument(
            f"{name} must be numeric",
            reason="testing_numeric_type",
            cause=error,
        ) from error
    if array.size > MAXIMUM_ASSERTION_ELEMENTS:
        raise ResourceExhausted(
            f"numeric assertion exceeds {MAXIMUM_ASSERTION_ELEMENTS} elements",
            reason="testing_numeric_elements",
        )
    return array


def assert_allclose(
    actual: object,
    expected: object,
    *,
    rtol: float = 1e-7,
    atol: float = 0.0,
    equal_nan: bool = False,
    require_finite: bool = True,
) -> None:
    """Assert equal shapes and elementwise closeness with bounded diagnostics."""
    if (
        isinstance(rtol, bool)
        or isinstance(atol, bool)
        or not isinstance(rtol, int | float)
        or not isinstance(atol, int | float)
        or not math.isfinite(rtol)
        or not math.isfinite(atol)
        or rtol < 0
        or atol < 0
    ):
        raise InvalidArgument(
            "numeric tolerances must be finite and non-negative",
            reason="testing_numeric_tolerance",
        )
    left = _array(actual, name="actual")
    right = _array(expected, name="expected")
    if left.shape != right.shape:
        raise AssertionError(f"shape mismatch: actual {left.shape}, expected {right.shape}")
    if require_finite and (not np.all(np.isfinite(left)) or not np.all(np.isfinite(right))):
        raise AssertionError("non-finite value encountered while require_finite=True")
    close = np.isclose(left, right, rtol=rtol, atol=atol, equal_nan=equal_nan)
    if bool(np.all(close)):
        return
    failing = np.flatnonzero(~close)
    first_flat = int(failing[0])
    first_index = np.unravel_index(first_flat, left.shape)
    absolute = np.abs(left - right)
    finite_absolute = absolute[np.isfinite(absolute)]
    maximum = float(np.max(finite_absolute)) if finite_absolute.size else math.inf
    raise AssertionError(
        f"arrays differ at {failing.size}/{left.size} elements; first index {first_index}: "
        f"actual={left[first_index]!r}, expected={right[first_index]!r}; "
        f"max absolute error={maximum:.6g}, rtol={rtol:.6g}, atol={atol:.6g}"
    )


def assert_rotation_matrix(value: object, *, atol: float = 1e-7) -> None:
    """Assert that ``value`` is a finite right-handed 3x3 rotation matrix."""
    matrix = _array(value, name="rotation")
    if matrix.shape != (3, 3):
        raise AssertionError(f"rotation matrix shape is {matrix.shape}, expected (3, 3)")
    if not np.all(np.isfinite(matrix)):
        raise AssertionError("rotation matrix contains a non-finite value")
    assert_allclose(matrix.T @ matrix, np.eye(3), rtol=0.0, atol=atol)
    determinant = float(np.linalg.det(matrix))
    if not math.isclose(determinant, 1.0, rel_tol=0.0, abs_tol=atol):
        raise AssertionError(f"rotation determinant is {determinant:.9g}, expected +1")
