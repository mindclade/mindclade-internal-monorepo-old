# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from kernels.tilelang.compiler.layouts import StridedLayout
from kernels.tilelang.compiler.tma import TensorMapSpec
from kernels.tilelang.targets import BLACKWELL_SM100, CDNA3_GFX942, HOPPER, resolve_target


def test_target_capabilities_are_backend_specific() -> None:
    assert HOPPER.capabilities.supports_tma
    assert HOPPER.capabilities.supports_wgmma
    assert not HOPPER.capabilities.supports_tmem
    assert BLACKWELL_SM100.capabilities.supports_tmem
    assert not CDNA3_GFX942.capabilities.supports_tma
    assert CDNA3_GFX942.capabilities.warp_size == 64
    assert resolve_target("cuda", "sm_90") == HOPPER
    assert resolve_target("cuda", "sm_89") is None


def test_tma_requires_alignment_rank_and_nvidia_capability() -> None:
    layout = StridedLayout.contiguous((128, 64), element_bytes=2, alignment=16)
    tensor_map = TensorMapSpec(layout, (64, 64))
    tensor_map.validate_target(HOPPER)
    with pytest.raises(ValueError, match="TMA-capable CUDA"):
        tensor_map.validate_target(CDNA3_GFX942)
    with pytest.raises(ValueError, match="alignment"):
        TensorMapSpec(
            StridedLayout.contiguous((128, 64), element_bytes=2, alignment=8),
            (64, 64),
        )
