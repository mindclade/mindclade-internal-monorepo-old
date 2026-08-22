# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Numerical, gradient, state, and export coverage for normalization modules."""

from collections.abc import Callable

import pytest
import torch
from torch import nn
from torch.nn import functional as F

from models.components.normalization import LayerNorm, RMSNorm


@pytest.mark.parametrize(
    ("dtype", "rtol", "atol"),
    [
        (torch.float32, 1e-6, 1e-6),
        (torch.float64, 1e-10, 1e-10),
        (torch.bfloat16, 2e-2, 2e-2),
    ],
)
def test_rms_norm_matches_fp32_or_better_reference_and_preserves_contract(
    dtype: torch.dtype,
    rtol: float,
    atol: float,
) -> None:
    model = RMSNorm(4, eps=1e-6, dtype=dtype)
    assert model.weight is not None
    with torch.no_grad():
        model.weight.copy_(torch.tensor([0.5, 1.0, 1.5, 2.0], dtype=dtype))
    inputs = (torch.arange(24, dtype=dtype).reshape(2, 4, 3).transpose(1, 2) - 7).div(5)
    assert not inputs.is_contiguous()

    output = model(inputs)

    reduction_dtype = torch.float64 if dtype is torch.float64 else torch.float32
    promoted = inputs.to(reduction_dtype)
    expected = (
        promoted * torch.rsqrt(promoted.square().mean(dim=-1, keepdim=True) + model.eps)
    ).to(dtype) * model.weight
    torch.testing.assert_close(output, expected, rtol=rtol, atol=atol)
    assert output.shape == inputs.shape
    assert output.dtype == inputs.dtype
    assert output.device == inputs.device


def test_rms_norm_supports_multiple_normalized_dimensions_without_affine_state() -> None:
    model = RMSNorm((2, 3), elementwise_affine=False)
    inputs = torch.arange(24, dtype=torch.float32).reshape(2, 2, 2, 3).transpose(0, 1) + 1

    output = model(inputs)

    mean_square = inputs.square().mean(dim=(-2, -1), keepdim=True)
    torch.testing.assert_close(
        output,
        inputs * torch.rsqrt(mean_square + model.eps),
        rtol=1e-6,
        atol=1e-6,
    )
    assert dict(model.named_parameters()) == {}
    assert model.state_dict() == {}


@pytest.mark.parametrize(
    ("bias", "expected_keys"), [(True, ["weight", "bias"]), (False, ["weight"])]
)
def test_layer_norm_matches_pytorch_and_uses_stable_state_keys(
    bias: bool,
    expected_keys: list[str],
) -> None:
    model = LayerNorm((2, 3), eps=2e-5, bias=bias, dtype=torch.float64)
    assert model.weight is not None
    with torch.no_grad():
        model.weight.copy_(torch.linspace(0.5, 1.5, 6, dtype=torch.float64).reshape(2, 3))
        if model.bias is not None:
            model.bias.copy_(torch.linspace(-0.3, 0.2, 6, dtype=torch.float64).reshape(2, 3))
    inputs = torch.randn(3, 4, 2, 3, dtype=torch.float64)[:, ::2]
    assert not inputs.is_contiguous()

    output = model(inputs)
    expected = F.layer_norm(inputs, (2, 3), model.weight, model.bias, model.eps)

    torch.testing.assert_close(output, expected, rtol=1e-12, atol=1e-12)
    assert list(model.state_dict()) == expected_keys
    assert dict(model.named_buffers()) == {}


def test_layer_norm_without_affine_state_is_parameter_free() -> None:
    model = LayerNorm(3, elementwise_affine=False)
    inputs = torch.randn(1, 2, 3)

    torch.testing.assert_close(
        model(inputs),
        F.layer_norm(inputs, (3,), eps=model.eps),
        rtol=1e-6,
        atol=1e-6,
    )
    assert dict(model.named_parameters()) == {}
    assert model.state_dict() == {}


@pytest.mark.parametrize("factory", [lambda: RMSNorm(4), lambda: LayerNorm(4)])
def test_backward_reaches_input_and_every_parameter_with_finite_gradients(
    factory: Callable[[], nn.Module],
) -> None:
    model = factory()
    inputs = torch.randn(2, 3, 4, requires_grad=True)

    model(inputs).square().mean().backward()

    assert inputs.grad is not None
    assert torch.isfinite(inputs.grad).all()
    for parameter in model.parameters():
        assert parameter.grad is not None
        assert torch.isfinite(parameter.grad).all()


def test_rms_norm_passes_double_precision_input_gradcheck() -> None:
    model = RMSNorm(3, elementwise_affine=False).double()
    inputs = torch.randn(2, 3, dtype=torch.float64, requires_grad=True)

    assert torch.autograd.gradcheck(model, (inputs,), eps=1e-6, atol=1e-5, rtol=1e-4)


@pytest.mark.parametrize("factory", [lambda: RMSNorm(4), lambda: LayerNorm(4, bias=True)])
def test_strict_state_dict_round_trip_preserves_output(
    factory: Callable[[], nn.Module],
) -> None:
    torch.manual_seed(17)
    source = factory()
    with torch.no_grad():
        for parameter in source.parameters():
            parameter.uniform_(-0.5, 1.5)
    inputs = torch.randn(1, 3, 4)
    expected = source(inputs)
    restored = factory()

    incompatible = restored.load_state_dict(source.state_dict(), strict=True)

    assert incompatible.missing_keys == []
    assert incompatible.unexpected_keys == []
    torch.testing.assert_close(restored(inputs), expected, rtol=1e-6, atol=1e-6)
    for source_parameter, restored_parameter in zip(
        source.parameters(), restored.parameters(), strict=True
    ):
        assert source_parameter.data_ptr() != restored_parameter.data_ptr()


@pytest.mark.parametrize("factory", [lambda: RMSNorm(4), lambda: LayerNorm(4)])
def test_train_and_eval_are_deterministic_and_equivalent(factory: Callable[[], nn.Module]) -> None:
    model = factory()
    inputs = torch.randn(1, 2, 4)
    model.train()
    train_first = model(inputs)
    train_second = model(inputs)
    model.eval()
    eval_first = model(inputs)
    eval_second = model(inputs)

    assert torch.equal(train_first, train_second)
    assert torch.equal(eval_first, eval_second)
    assert torch.equal(train_first, eval_first)


@pytest.mark.parametrize("factory", [lambda: RMSNorm(4), lambda: LayerNorm(4)])
def test_static_torch_export_matches_eager(factory: Callable[[], nn.Module]) -> None:
    model = factory().eval()
    inputs = torch.randn(1, 3, 4)

    exported = torch.export.export(model, (inputs,)).module()

    torch.testing.assert_close(exported(inputs), model(inputs), rtol=1e-6, atol=1e-6)


@pytest.mark.parametrize(
    ("factory", "error", "message"),
    [
        (lambda: RMSNorm(0), ValueError, "positive"),
        (lambda: RMSNorm(()), ValueError, "rank"),
        (lambda: RMSNorm((2, "3")), TypeError, "integers"),  # type: ignore[arg-type]
        (lambda: RMSNorm((1,) * 9), ValueError, "rank"),
        (lambda: RMSNorm((4097, 4097)), ValueError, "element bound"),
        (lambda: RMSNorm(4, eps=float("nan")), ValueError, "finite and positive"),
        (lambda: LayerNorm(4, bias=1), TypeError, "booleans"),  # type: ignore[arg-type]
    ],
)
def test_invalid_configuration_fails_before_parameter_allocation(
    factory: Callable[[], nn.Module],
    error: type[Exception],
    message: str,
) -> None:
    with pytest.raises(error, match=message):
        factory()


@pytest.mark.parametrize("factory", [lambda: RMSNorm(4), lambda: LayerNorm(4)])
def test_invalid_inputs_fail_at_the_module_boundary(factory: Callable[[], nn.Module]) -> None:
    model = factory()
    with pytest.raises(TypeError, match=r"torch\.Tensor"):
        model([1.0, 2.0, 3.0, 4.0])
    with pytest.raises(TypeError, match="floating-point"):
        model(torch.ones(2, 4, dtype=torch.int64))
    with pytest.raises(ValueError, match="nonempty"):
        model(torch.empty(0, 4))
    with pytest.raises(ValueError, match="trailing dimensions"):
        model(torch.ones(2, 3))
    with pytest.raises(TypeError, match="same dtype"):
        model(torch.ones(2, 4, dtype=torch.float64))
    with pytest.raises(ValueError, match="same device"):
        model(torch.ones(2, 4, device="meta"))


def test_nonfinite_values_follow_pytorch_propagation_policy() -> None:
    inputs = torch.tensor([[1.0, float("nan"), 2.0]])

    assert torch.isnan(RMSNorm(3)(inputs)).all()
    assert torch.isnan(LayerNorm(3)(inputs)).all()
