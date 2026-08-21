# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest
import torch

from kernels.ops.diffusion import modulated_residual_reference, neighbor_attention_reference


def test_modulated_residual_fuses_exact_broadcast_order() -> None:
    torch.manual_seed(41)
    normalized = torch.randn(2, 3, 5)
    residual = torch.randn_like(normalized)
    scale, shift, gate = (torch.randn(2, 5) for _ in range(3))
    actual = modulated_residual_reference(normalized, residual, scale, shift, gate)
    expected = residual + gate[:, None] * (normalized * (1 + scale[:, None]) + shift[:, None])
    torch.testing.assert_close(actual, expected)


def test_neighbor_attention_matches_dense_masked_reference_and_zero_empty_rows() -> None:
    torch.manual_seed(43)
    q = torch.randn(1, 2, 4, 8)
    k = torch.randn_like(q)
    v = torch.randn_like(q)
    neighbors = torch.tensor([[[-1, -1, -1], [0, 1, -1], [1, 3, -1], [0, 2, 3]]])
    actual = neighbor_attention_reference(q, k, v, neighbors)
    assert torch.equal(actual[:, :, 0], torch.zeros_like(actual[:, :, 0]))

    for query in range(1, 4):
        indices = neighbors[0, query]
        indices = indices[indices >= 0]
        scores = (q[0, :, query, None].float() * k[0, :, indices].float()).sum(-1) / 8**0.5
        probabilities = torch.softmax(scores, dim=-1)
        expected = torch.einsum("hk,hkd->hd", probabilities, v[0, :, indices].float())
        torch.testing.assert_close(actual[0, :, query], expected, rtol=2e-5, atol=2e-6)


def test_neighbor_indices_fail_closed() -> None:
    q = torch.randn(1, 1, 2, 4)
    with pytest.raises(RuntimeError, match="within the sequence"):
        neighbor_attention_reference(q, q, q, torch.tensor([[[0], [2]]]))
