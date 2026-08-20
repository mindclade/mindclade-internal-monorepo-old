# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import numpy as np
import pytest

from libs.python.testing import assert_allclose, assert_rotation_matrix


def test_allclose_reports_shape_and_value_mismatches() -> None:
    assert_allclose([1.0, 2.0], [1.0, 2.00000001], atol=1e-7)

    with pytest.raises(AssertionError, match="shape mismatch"):
        assert_allclose([1.0], [[1.0]])
    with pytest.raises(AssertionError, match="first index"):
        assert_allclose([1.0, 2.0], [1.0, 3.0])


def test_rotation_matrix_checks_handedness() -> None:
    assert_rotation_matrix(np.eye(3))
    reflection = np.diag([-1.0, 1.0, 1.0])
    with pytest.raises(AssertionError, match="determinant"):
        assert_rotation_matrix(reflection)
