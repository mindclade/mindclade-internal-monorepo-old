# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import math

import numpy as np
import pytest

from libs.python.geometry import RigidTransform, rotation_about_axis


def test_rigid_apply_inverse_and_composition() -> None:
    rotate = rotation_about_axis([0, 0, 1], math.pi / 2)
    translate = RigidTransform(np.eye(3), np.array([1.0, 2.0, 3.0]))
    composed = translate.compose(rotate)
    point = np.array([[1.0, 0.0, 0.0]])

    np.testing.assert_allclose(composed.apply(point), [[1.0, 3.0, 3.0]], atol=1e-12)
    np.testing.assert_allclose(composed.inverse().apply(composed.apply(point)), point, atol=1e-12)
    assert composed.inverse().compose(composed).almost_equals(RigidTransform.identity())


def test_rigid_snapshots_arrays_and_rejects_reflections() -> None:
    translation = np.array([1.0, 2.0, 3.0])
    transform = RigidTransform(np.eye(3), translation)
    translation[0] = 99
    assert transform.translation[0] == 1
    with pytest.raises(ValueError, match="right-handed"):
        RigidTransform(np.diag([-1.0, 1.0, 1.0]), np.zeros(3))
    with pytest.raises(ValueError, match="finite"):
        transform.apply([[math.nan, 0, 0]])
