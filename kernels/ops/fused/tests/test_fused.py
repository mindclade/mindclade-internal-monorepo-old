# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from typing import Literal

import pytest
import torch

from kernels.ops.fused import swiglu_reference, triangle_multiplication_reference


def test_swiglu_matches_decomposed_equation_and_preserves_dtype() -> None:
    torch.manual_seed(11)
    gate = torch.randn(3, 17, dtype=torch.bfloat16)
    up = torch.randn(3, 17, dtype=torch.bfloat16)
    actual = swiglu_reference(gate, up)
    expected = (torch.nn.functional.silu(gate.float()) * up.float()).to(torch.bfloat16)
    assert actual.dtype == torch.bfloat16
    torch.testing.assert_close(actual, expected, rtol=0, atol=0)


@pytest.mark.parametrize("orientation", ["incoming", "outgoing"])
def test_triangle_multiplication_matches_loop_reference_and_masks_padding(
    orientation: Literal["incoming", "outgoing"],
) -> None:
    torch.manual_seed(23)
    left = torch.randn(1, 4, 4, 3)
    right = torch.randn(1, 4, 4, 3)
    mask = torch.tensor([[True, True, False, True]])
    actual = triangle_multiplication_reference(left, right, mask, orientation=orientation)
    expected = torch.zeros_like(actual)
    for i in range(4):
        for j in range(4):
            if not mask[0, i] or not mask[0, j]:
                continue
            for k in range(4):
                if not mask[0, k]:
                    continue
                if orientation == "outgoing":
                    expected[0, i, j] += left[0, i, k] * right[0, j, k]
                else:
                    expected[0, i, j] += left[0, k, i] * right[0, k, j]
    torch.testing.assert_close(actual, expected, rtol=2e-5, atol=2e-6)
