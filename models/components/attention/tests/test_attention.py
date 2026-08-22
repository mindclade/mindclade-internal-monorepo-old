# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Contract, numerical, gradient, state, fake-tensor, and export tests."""

from __future__ import annotations

import copy

import pytest
import torch
from torch import nn
from torch._subclasses.fake_tensor import FakeTensor, FakeTensorMode
from torch.nn import functional as F

from models.components.attention import (
    AttentionOperator,
    DenseMultiheadAttention,
    PyTorchSDPAOperator,
    RotaryEmbedding,
)

RTOL = 1e-5
ATOL = 1e-6


def _identity_attention(*, bias: bool = False) -> DenseMultiheadAttention:
    module = DenseMultiheadAttention(4, 2, bias=bias)
    with torch.no_grad():
        identity = torch.eye(4)
        for projection in (
            module.query_projection,
            module.key_projection,
            module.value_projection,
            module.output_projection,
        ):
            projection.weight.copy_(identity)
            if projection.bias is not None:
                projection.bias.zero_()
    return module


def _heads(value: torch.Tensor) -> torch.Tensor:
    return value.reshape(value.shape[0], value.shape[1], 2, 2).transpose(1, 2)


def test_dense_attention_matches_direct_pytorch_sdpa() -> None:
    torch.manual_seed(7)
    module = _identity_attention().eval()
    query = torch.randn(2, 3, 4)
    key = torch.randn(2, 5, 4)
    value = torch.randn(2, 5, 4)
    mask = torch.tensor([[True, True, False, True, False], [True, False, True, True, True]])
    actual = module(query, key, value, mask)
    expected_heads = F.scaled_dot_product_attention(
        _heads(query),
        _heads(key),
        _heads(value),
        attn_mask=mask[:, None, None, :],
    )
    expected = expected_heads.transpose(1, 2).reshape(2, 3, 4)
    torch.testing.assert_close(actual, expected, rtol=RTOL, atol=ATOL)
    assert actual.shape == query.shape
    assert actual.dtype == query.dtype
    assert actual.device == query.device


@pytest.mark.parametrize(
    ("constructor", "message"),
    [
        (
            lambda: DenseMultiheadAttention("4", 2),  # type: ignore[arg-type]
            "embed_dim must be an integer",
        ),
        (
            lambda: DenseMultiheadAttention(4, 2.0),  # type: ignore[arg-type]
            "num_heads must be an integer",
        ),
        (lambda: DenseMultiheadAttention(4, 2, dropout=True), "dropout must be a real"),
        (
            lambda: DenseMultiheadAttention(4, 2, scale="auto"),  # type: ignore[arg-type]
            "scale must be a real",
        ),
        (
            lambda: RotaryEmbedding(4.0),  # type: ignore[arg-type]
            "head_dim must be an integer",
        ),
        (lambda: RotaryEmbedding(4, base=True), "rotary base must be a real"),
    ],
)
def test_constructor_contracts_reject_ambiguous_scalar_types(
    constructor: object,
    message: str,
) -> None:
    assert callable(constructor)
    with pytest.raises(TypeError, match=message):
        constructor()


def test_explicit_causal_mask_and_all_masked_rows_are_finite_zero() -> None:
    torch.manual_seed(11)
    module = _identity_attention(bias=True).eval()
    with torch.no_grad():
        module.output_projection.bias.fill_(3.0)
    values = torch.randn(1, 4, 4)
    mask = torch.ones(1, 4, 4, dtype=torch.bool)
    mask[:, 2, :] = False
    output = module(values, values, values, mask, causal=True)
    assert torch.isfinite(output).all()
    torch.testing.assert_close(output[:, 2], torch.zeros_like(output[:, 2]), rtol=0, atol=0)


@pytest.mark.parametrize(
    ("query", "key", "value", "mask", "message"),
    [
        (
            torch.ones(1, 2, 4, dtype=torch.int64),
            torch.ones(1, 2, 4, dtype=torch.int64),
            torch.ones(1, 2, 4, dtype=torch.int64),
            None,
            "floating-point",
        ),
        (
            torch.randn(1, 2, 4),
            torch.randn(1, 3, 4),
            torch.randn(1, 2, 4),
            None,
            "sequence lengths",
        ),
        (
            torch.randn(1, 2, 4),
            torch.randn(1, 2, 4),
            torch.randn(1, 2, 4),
            torch.ones(1, 2),
            "boolean dtype",
        ),
        (
            torch.randn(1, 2, 4),
            torch.randn(1, 2, 4),
            torch.randn(1, 2, 4),
            torch.ones(1, 3, dtype=torch.bool),
            r"shape \[B, Tk\]",
        ),
    ],
)
def test_invalid_tensor_and_mask_contracts_fail_at_boundary(
    query: torch.Tensor,
    key: torch.Tensor,
    value: torch.Tensor,
    mask: torch.Tensor | None,
    message: str,
) -> None:
    with pytest.raises((TypeError, ValueError), match=message):
        DenseMultiheadAttention(4, 2)(query, key, value, mask)


def test_backward_reaches_inputs_and_every_projection_parameter() -> None:
    torch.manual_seed(13)
    module = DenseMultiheadAttention(8, 2, dropout=0.0)
    query = torch.randn(2, 3, 8, requires_grad=True)
    key = torch.randn(2, 4, 8, requires_grad=True)
    value = torch.randn(2, 4, 8, requires_grad=True)
    mask = torch.ones(2, 3, 4, dtype=torch.bool)
    module(query, key, value, mask).square().mean().backward()
    for tensor in (query, key, value):
        assert tensor.grad is not None
        assert torch.isfinite(tensor.grad).all()
    for name, parameter in module.named_parameters():
        assert parameter.grad is not None, name
        assert torch.isfinite(parameter.grad).all(), name


class GainOperator(AttentionOperator):
    def __init__(self) -> None:
        super().__init__()
        self.gain = nn.Parameter(torch.tensor(2.0))

    def forward(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
        *,
        mask: torch.Tensor | None,
        causal: bool,
        scale: float | None,
        dropout_p: float,
    ) -> torch.Tensor:
        del key, value, mask, causal, scale, dropout_p
        return query * self.gain


def test_injected_operator_is_registered_and_all_masked_semantics_are_enforced() -> None:
    module = _identity_attention()
    module.operator = GainOperator()
    values = torch.randn(1, 2, 4)
    mask = torch.tensor([[[False, False], [True, True]]])
    output = module(values, values, values, mask)
    assert "operator.gain" in dict(module.named_parameters())
    torch.testing.assert_close(output[:, 0], torch.zeros_like(output[:, 0]), rtol=0, atol=0)
    output.sum().backward()
    assert module.operator.gain.grad is not None


def test_eval_is_deterministic_and_state_dict_round_trip_is_strict() -> None:
    torch.manual_seed(17)
    original = DenseMultiheadAttention(8, 2, dropout=0.5).eval()
    restored = DenseMultiheadAttention(8, 2, dropout=0.5).eval()
    restored.load_state_dict(copy.deepcopy(original.state_dict()), strict=True)
    values = torch.randn(1, 3, 8)
    first = original(values, values, values)
    second = original(values, values, values)
    round_trip = restored(values, values, values)
    torch.testing.assert_close(first, second, rtol=0, atol=0)
    torch.testing.assert_close(first, round_trip, rtol=RTOL, atol=ATOL)
    assert isinstance(original.operator, PyTorchSDPAOperator)


def test_rotary_embedding_preserves_pair_norm_and_has_finite_gradients() -> None:
    torch.manual_seed(19)
    rotary = RotaryEmbedding(6)
    value = torch.randn(2, 3, 4, 6, requires_grad=True)
    positions = torch.tensor([0, 2, 5, 9])
    output = rotary(value, positions)
    torch.testing.assert_close(output[..., 0, :], value[..., 0, :], rtol=RTOL, atol=ATOL)
    original_pair_norm = value.reshape(2, 3, 4, 3, 2).square().sum(dim=-1)
    rotated_pair_norm = output.reshape(2, 3, 4, 3, 2).square().sum(dim=-1)
    torch.testing.assert_close(rotated_pair_norm, original_pair_norm, rtol=RTOL, atol=ATOL)
    output.sum().backward()
    assert value.grad is not None
    assert torch.isfinite(value.grad).all()
    assert "inverse_frequencies" in rotary.state_dict()


def test_attention_is_fake_tensor_friendly() -> None:
    module = DenseMultiheadAttention(8, 2).eval()
    mode = FakeTensorMode(allow_non_fake_inputs=True)
    query = mode.from_tensor(torch.randn(2, 3, 8))
    key = mode.from_tensor(torch.randn(2, 5, 8))
    value = mode.from_tensor(torch.randn(2, 5, 8))
    mask = mode.from_tensor(torch.ones(2, 3, 5, dtype=torch.bool))
    with mode:
        output = module(query, key, value, mask)
    assert isinstance(output, FakeTensor)
    assert output.shape == (2, 3, 8)


def test_attention_torch_export_parity() -> None:
    torch.manual_seed(23)
    module = DenseMultiheadAttention(8, 2).eval()
    query = torch.randn(1, 3, 8)
    key = torch.randn(1, 4, 8)
    value = torch.randn(1, 4, 8)
    mask = torch.ones(1, 3, 4, dtype=torch.bool)
    eager = module(query, key, value, mask)
    exported = torch.export.export(module, (query, key, value, mask))
    actual = exported.module()(query, key, value, mask)
    torch.testing.assert_close(actual, eager, rtol=RTOL, atol=ATOL)
