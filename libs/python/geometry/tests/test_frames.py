# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import numpy as np
import pytest

from libs.python.geometry import frame_from_axes


def test_frame_from_axes_is_right_handed_and_places_origin() -> None:
    frame = frame_from_axes([1, 2, 3], [2, 0, 0], [1, 3, 0])
    np.testing.assert_allclose(frame.rotation, np.eye(3), atol=1e-12)
    np.testing.assert_allclose(frame.apply([[0, 0, 0]]), [[1, 2, 3]], atol=1e-12)


def test_frame_rejects_degenerate_axes() -> None:
    with pytest.raises(ValueError, match="non-zero"):
        frame_from_axes([0, 0, 0], [0, 0, 0], [0, 1, 0])
    with pytest.raises(ValueError, match="parallel"):
        frame_from_axes([0, 0, 0], [1, 0, 0], [2, 0, 0])
