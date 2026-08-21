# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest
import torch
import torch.nn.functional as F

from kernels.ops.attention import attention_reference


@pytest.mark.parametrize("causal", [False, True])
@pytest.mark.parametrize("shape", [(1, 1, 1, 8), (2, 3, 7, 16), (1, 2, 17, 32)])
def test_attention_matches_framework_sdpa(shape: tuple[int, ...], causal: bool) -> None:
    torch.manual_seed(20260820)
    q = torch.randn(shape)
    k = torch.randn(shape)
    v = torch.randn(shape)
    actual = attention_reference(q, k, v, causal=causal)
    expected = F.scaled_dot_product_attention(q, k, v, is_causal=causal)
    torch.testing.assert_close(actual, expected, rtol=2e-5, atol=2e-6)


def test_attention_composes_masks_and_defines_all_masked_rows_as_zero() -> None:
    torch.manual_seed(17)
    q = torch.randn(1, 2, 3, 8)
    k = torch.randn(1, 2, 5, 8)
    v = torch.randn(1, 2, 5, 8)
    mask = torch.tensor([[[[False] * 5, [True, False, True, False, True], [True] * 5]]])
    output = attention_reference(q, k, v, mask=mask)
    assert torch.equal(output[:, :, 0], torch.zeros_like(output[:, :, 0]))
    assert torch.isfinite(output).all()


def test_attention_rejects_incompatible_shapes_and_mask_dtype() -> None:
    q = torch.randn(1, 1, 2, 8)
    with pytest.raises(RuntimeError, match="identical BHSD"):
        attention_reference(q, torch.randn(1, 1, 3, 8), torch.randn(1, 1, 4, 8))
    with pytest.raises(RuntimeError, match="boolean"):
        attention_reference(q, q, q, mask=torch.ones(2, 2))


def test_attention_reference_passes_double_precision_gradcheck() -> None:
    torch.manual_seed(5)
    tensors = tuple(
        torch.randn(1, 1, 2, 3, dtype=torch.float64, requires_grad=True) for _ in range(3)
    )
    assert torch.autograd.gradcheck(lambda q, k, v: attention_reference(q, k, v), tensors)
