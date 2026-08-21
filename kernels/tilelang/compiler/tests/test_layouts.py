# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest

from kernels.tilelang.compiler.layouts import SharedTile, StridedLayout
from kernels.tilelang.compiler.swizzle import SharedSwizzle


def test_contiguous_layout_derives_strides_vector_width_and_bytes() -> None:
    layout = StridedLayout.contiguous((3, 8, 16), element_bytes=2, alignment=16)
    assert layout.strides == (128, 16, 1)
    assert layout.nbytes == 768
    assert layout.vector_width() == 8


def test_shared_swizzle_validates_producer_consumer_row_shape() -> None:
    tile = SharedTile(64, 64, 2)
    SharedSwizzle(128).validate(tile)
    with pytest.raises(ValueError, match="span"):
        SharedSwizzle(128).validate(SharedTile(16, 16, 2))
