# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Contract, gradient, state, and export coverage for reusable NN primitives."""

import pytest
import torch
from torch.nn import functional as F

from models.components.nn import (
    ResidualAdd,
    SwiGLU,
    SwiGLUFeedForward,
    residual_add,
    swiglu,
)


def test_swiglu_matches_fp32_reference_on_noncontiguous_inputs() -> None:
    gate = torch.linspace(-4, 4, 24).reshape(2, 4, 3).transpose(1, 2)
    up = torch.linspace(2, -2, 24).reshape(2, 4, 3).transpose(1, 2)
    assert not gate.is_contiguous()

    output = swiglu(gate, up)
    expected = (F.silu(gate.float()) * up.float()).to(gate.dtype)

    torch.testing.assert_close(output, expected, rtol=1e-6, atol=1e-6)
    assert output.shape == gate.shape
    assert output.dtype == gate.dtype
    assert output.device == gate.device
    assert SwiGLU().state_dict() == {}


def test_swiglu_preserves_float64_compute_precision() -> None:
    gate = torch.tensor([1.0e-12, -17.25, 19.75], dtype=torch.float64)
    up = torch.tensor([1.0e12, 0.125, -0.25], dtype=torch.float64)

    output = swiglu(gate, up)
    expected = F.silu(gate) * up

    torch.testing.assert_close(output, expected, rtol=1e-14, atol=1e-14)
    assert output.dtype is torch.float64


def test_swiglu_rejects_invalid_tensor_pairs() -> None:
    with pytest.raises(ValueError, match="identical shapes"):
        swiglu(torch.ones(2, 3), torch.ones(2, 4))
    with pytest.raises(TypeError, match="floating-point"):
        swiglu(torch.ones(2, dtype=torch.int64), torch.ones(2, dtype=torch.int64))
    with pytest.raises(ValueError, match="nonempty"):
        swiglu(torch.empty(0), torch.empty(0))
    with pytest.raises(ValueError, match="same device"):
        swiglu(torch.ones(2), torch.ones(2, device="meta"))


@pytest.mark.parametrize("bias", [False, True])
def test_feed_forward_matches_decomposed_equation_and_preserves_shape(bias: bool) -> None:
    torch.manual_seed(23)
    model = SwiGLUFeedForward(4, 7, bias=bias)
    inputs = torch.randn(2, 5, 4).transpose(0, 1)
    assert not inputs.is_contiguous()

    output = model(inputs)
    expected = F.linear(
        swiglu(
            F.linear(inputs, model.gate_proj.weight, model.gate_proj.bias),
            F.linear(inputs, model.up_proj.weight, model.up_proj.bias),
        ),
        model.down_proj.weight,
        model.down_proj.bias,
    )

    torch.testing.assert_close(output, expected, rtol=1e-6, atol=1e-6)
    assert output.shape == inputs.shape
    assert output.dtype == inputs.dtype
    assert output.device == inputs.device


def test_feed_forward_parameter_registration_and_state_keys_are_stable() -> None:
    without_bias = SwiGLUFeedForward(4, 6)
    with_bias = SwiGLUFeedForward(4, 6, bias=True)

    assert list(without_bias.state_dict()) == [
        "gate_proj.weight",
        "up_proj.weight",
        "down_proj.weight",
    ]
    assert list(with_bias.state_dict()) == [
        "gate_proj.weight",
        "gate_proj.bias",
        "up_proj.weight",
        "up_proj.bias",
        "down_proj.weight",
        "down_proj.bias",
    ]
    assert dict(with_bias.named_buffers()) == {}
    assert sum(parameter.numel() for parameter in without_bias.parameters()) == 4 * 6 * 3


def test_feed_forward_backward_reaches_input_and_all_parameters() -> None:
    model = SwiGLUFeedForward(4, 8, bias=True)
    inputs = torch.randn(1, 3, 4, requires_grad=True)

    model(inputs).square().mean().backward()

    assert inputs.grad is not None
    assert torch.isfinite(inputs.grad).all()
    for parameter in model.parameters():
        assert parameter.grad is not None
        assert torch.isfinite(parameter.grad).all()


def test_feed_forward_strict_state_dict_round_trip_preserves_output() -> None:
    torch.manual_seed(29)
    source = SwiGLUFeedForward(4, 9, bias=True)
    restored = SwiGLUFeedForward(4, 9, bias=True)
    inputs = torch.randn(1, 2, 4)

    incompatible = restored.load_state_dict(source.state_dict(), strict=True)

    assert incompatible.missing_keys == []
    assert incompatible.unexpected_keys == []
    torch.testing.assert_close(restored(inputs), source(inputs), rtol=1e-6, atol=1e-6)
    for source_parameter, restored_parameter in zip(
        source.parameters(), restored.parameters(), strict=True
    ):
        assert source_parameter.data_ptr() != restored_parameter.data_ptr()


def test_feed_forward_train_and_eval_are_deterministic_and_equivalent() -> None:
    model = SwiGLUFeedForward(4, 8)
    inputs = torch.randn(1, 3, 4)
    model.train()
    train_first = model(inputs)
    train_second = model(inputs)
    model.eval()
    eval_first = model(inputs)
    eval_second = model(inputs)

    assert torch.equal(train_first, train_second)
    assert torch.equal(eval_first, eval_second)
    assert torch.equal(train_first, eval_first)


def test_feed_forward_supports_bfloat16_with_fp32_activation() -> None:
    model = SwiGLUFeedForward(4, 8, dtype=torch.bfloat16)
    inputs = torch.randn(1, 2, 4, dtype=torch.bfloat16)

    output = model(inputs)

    assert output.dtype is torch.bfloat16
    assert output.shape == inputs.shape
    assert torch.isfinite(output).all()


@pytest.mark.parametrize(
    ("arguments", "error", "message"),
    [
        ((0, 4), ValueError, "hidden_size"),
        ((4, 0), ValueError, "intermediate_size"),
        ((True, 4), TypeError, "hidden_size"),
        ((4, 1_048_577), ValueError, "intermediate_size"),
    ],
)
def test_feed_forward_rejects_invalid_configuration(
    arguments: tuple[int, int],
    error: type[Exception],
    message: str,
) -> None:
    with pytest.raises(error, match=message):
        SwiGLUFeedForward(*arguments)
    with pytest.raises(TypeError, match="bias"):
        SwiGLUFeedForward(4, 8, bias=1)  # type: ignore[arg-type]


def test_feed_forward_rejects_invalid_inputs_at_the_boundary() -> None:
    model = SwiGLUFeedForward(4, 8)
    with pytest.raises(TypeError, match=r"torch\.Tensor"):
        model([1.0, 2.0, 3.0, 4.0])
    with pytest.raises(TypeError, match="floating-point"):
        model(torch.ones(2, 4, dtype=torch.int64))
    with pytest.raises(ValueError, match="nonempty"):
        model(torch.empty(0, 4))
    with pytest.raises(ValueError, match="feature dimension"):
        model(torch.ones(2, 3))
    with pytest.raises(TypeError, match="same dtype"):
        model(torch.ones(2, 4, dtype=torch.float64))
    with pytest.raises(ValueError, match="same device"):
        model(torch.ones(2, 4, device="meta"))


def test_residual_add_has_strict_nonbroadcasting_semantics_and_gradients() -> None:
    residual = torch.randn(1, 3, 4, requires_grad=True)
    update = torch.randn(1, 3, 4, requires_grad=True)

    output = residual_add(residual, update, scale=0.25)
    torch.autograd.backward(output.sum())

    torch.testing.assert_close(output, residual.detach() + 0.25 * update.detach())
    torch.testing.assert_close(residual.grad, torch.ones_like(residual))
    torch.testing.assert_close(update.grad, torch.full_like(update, 0.25))
    assert ResidualAdd(0.25).state_dict() == {}


def test_residual_add_rejects_invalid_inputs_and_scale() -> None:
    with pytest.raises(ValueError, match="identical shapes"):
        residual_add(torch.ones(2, 1), torch.ones(2))
    with pytest.raises(TypeError, match="same dtype"):
        residual_add(torch.ones(2), torch.ones(2, dtype=torch.float64))
    with pytest.raises(TypeError, match="floating-point"):
        residual_add(torch.ones(2, dtype=torch.int64), torch.ones(2, dtype=torch.int64))
    with pytest.raises(ValueError, match="nonempty"):
        residual_add(torch.empty(0), torch.empty(0))
    with pytest.raises(ValueError, match="finite"):
        ResidualAdd(float("nan"))
    with pytest.raises(TypeError, match="real number"):
        ResidualAdd(True)


@pytest.mark.parametrize(
    ("module", "arguments"),
    [
        (SwiGLU(), (torch.randn(1, 3, 4), torch.randn(1, 3, 4))),
        (SwiGLUFeedForward(4, 8).eval(), (torch.randn(1, 3, 4),)),
        (ResidualAdd(0.5), (torch.randn(1, 3, 4), torch.randn(1, 3, 4))),
    ],
)
def test_static_torch_export_matches_eager(
    module: torch.nn.Module,
    arguments: tuple[torch.Tensor, ...],
) -> None:
    exported = torch.export.export(module, arguments).module()

    torch.testing.assert_close(exported(*arguments), module(*arguments), rtol=1e-6, atol=1e-6)
