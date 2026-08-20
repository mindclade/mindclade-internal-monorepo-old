# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import numpy as np
import pytest

from libs.python.geometry import pairwise_distances, root_mean_square_deviation


def test_pairwise_distances_and_rmsd() -> None:
    points = np.array([[0.0, 0.0, 0.0], [3.0, 4.0, 0.0]])
    np.testing.assert_allclose(pairwise_distances(points), [[0.0, 5.0], [5.0, 0.0]])
    assert root_mean_square_deviation(points, points + 1.0) == pytest.approx(np.sqrt(3.0))


def test_distance_inputs_must_be_compatible_and_finite() -> None:
    with pytest.raises(ValueError, match="same non-empty shape"):
        root_mean_square_deviation([[0, 0, 0]], [[0, 0, 0], [1, 1, 1]])
    with pytest.raises(ValueError, match="finite"):
        pairwise_distances([[np.inf, 0, 0]])
