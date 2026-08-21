# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest
import torch

from kernels.ops.moe import expert_capacity, padded_grouped_gemm_reference, stable_topk


def test_stable_topk_uses_lower_expert_index_for_ties_and_normalizes_weights() -> None:
    logits = torch.tensor([[2.0, 2.0, 1.0], [0.0, 3.0, 3.0]])
    decision = stable_topk(logits, 2)
    assert decision.expert_indices.tolist() == [[0, 1], [1, 2]]
    torch.testing.assert_close(decision.weights.sum(dim=-1), torch.ones(2))


def test_capacity_is_bounded_and_validated() -> None:
    assert expert_capacity(100, 8, 2, capacity_factor=1.25) == 32
    with pytest.raises(ValueError, match="valid positive"):
        expert_capacity(0, 8, 2)


def test_padded_grouped_gemm_matches_independent_per_expert_matmul() -> None:
    torch.manual_seed(31)
    inputs = torch.randn(3, 5, 7)
    weights = torch.randn(3, 7, 4)
    actual = padded_grouped_gemm_reference(inputs, weights)
    expected = torch.stack([inputs[index] @ weights[index] for index in range(3)])
    torch.testing.assert_close(actual, expected, rtol=2e-5, atol=2e-6)
