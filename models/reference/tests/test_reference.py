# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Contract tests for the deterministic reference affine module."""

import json
from dataclasses import FrozenInstanceError
from pathlib import Path

import pytest
import torch
from safetensors.torch import load_file, save_file

from models.reference import (
    DEFAULT_MAXIMUM_INPUT_ELEMENTS,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    ReferenceAffineConfig,
    load_reference_affine,
    parse_reference_affine_config,
    reference_affine_config_bytes,
    reference_affine_config_document,
    save_reference_affine,
)


def test_batch_size_one_preserves_tensor_contract() -> None:
    model = ReferenceAffine()
    inputs = torch.tensor([[1.0, -2.0, 3.5]], dtype=torch.float32)

    output = model(inputs)

    torch.testing.assert_close(output, torch.tensor([[2.5, -3.5, 7.5]]))
    assert output.shape == inputs.shape
    assert output.dtype == inputs.dtype
    assert output.device == inputs.device


def test_noncontiguous_input_is_supported_and_shape_preserved() -> None:
    model = ReferenceAffine()
    inputs = torch.arange(12, dtype=torch.float32).reshape(3, 4).transpose(0, 1)
    assert not inputs.is_contiguous()

    output = model(inputs)

    torch.testing.assert_close(output, (inputs * 2.0) + 0.5)
    assert output.shape == inputs.shape


def test_validated_forward_and_trusted_adapter_compute_have_exact_parity() -> None:
    model = ReferenceAffine()
    inputs = torch.tensor([[-1.5, 0.0, 2.25]], dtype=torch.float32)

    torch.testing.assert_close(model.compute(inputs), model(inputs), rtol=0.0, atol=0.0)


def test_invalid_input_type_is_rejected() -> None:
    model = ReferenceAffine()

    with pytest.raises(TypeError, match=r"torch\.Tensor"):
        model([1.0])


def test_invalid_input_dtype_is_rejected_without_casting() -> None:
    model = ReferenceAffine()

    with pytest.raises(TypeError, match=r"torch\.float32"):
        model(torch.ones((2, 2), dtype=torch.float64))


def test_empty_input_is_rejected() -> None:
    model = ReferenceAffine()

    with pytest.raises(ValueError, match="nonempty"):
        model(torch.empty((1, 0), dtype=torch.float32))


def test_nonfinite_input_is_rejected() -> None:
    model = ReferenceAffine()

    with pytest.raises(ValueError, match="finite"):
        model(torch.tensor([float("inf")], dtype=torch.float32))


def test_finite_input_that_overflows_arithmetic_is_rejected() -> None:
    model = ReferenceAffine()

    with pytest.raises(FloatingPointError, match="non-finite output"):
        model(torch.tensor([torch.finfo(torch.float32).max], dtype=torch.float32))


def test_input_on_different_device_is_rejected_without_transfer() -> None:
    model = ReferenceAffine()
    inputs = torch.empty((1,), dtype=torch.float32, device="meta")

    with pytest.raises(ValueError, match="same device"):
        model(inputs)


def test_input_element_budget_is_enforced() -> None:
    model = ReferenceAffine(ReferenceAffineConfig(maximum_input_elements=2))

    with pytest.raises(ValueError, match="maximum_input_elements"):
        model(torch.ones((3,), dtype=torch.float32))


def test_forward_and_backward_produce_finite_gradients() -> None:
    model = ReferenceAffine()
    inputs = torch.tensor([-2.0, 0.0, 4.0], dtype=torch.float32, requires_grad=True)

    loss = model(inputs).square().mean()
    loss.backward()

    assert torch.isfinite(loss)
    assert inputs.grad is not None
    assert torch.isfinite(inputs.grad).all()
    assert model.scale.grad is not None
    assert torch.isfinite(model.scale.grad).all()
    assert model.bias.grad is not None
    assert torch.isfinite(model.bias.grad).all()


def test_parameters_are_exact_registered_scalar_float32_state() -> None:
    model = ReferenceAffine()
    parameters = dict(model.named_parameters())

    assert list(parameters) == ["scale", "bias"]
    assert list(model.state_dict()) == ["scale", "bias"]
    assert dict(model.named_buffers()) == {}
    assert sum(parameter.numel() for parameter in parameters.values()) == 2
    for parameter in parameters.values():
        assert isinstance(parameter, torch.nn.Parameter)
        assert parameter.shape == torch.Size([])
        assert parameter.dtype == torch.float32


def test_train_and_eval_modes_are_deterministic_and_equivalent() -> None:
    model = ReferenceAffine()
    inputs = torch.tensor([[1.0, 2.0]], dtype=torch.float32)
    state_before = {name: value.clone() for name, value in model.state_dict().items()}

    model.train()
    first_train = model(inputs)
    second_train = model(inputs)
    model.eval()
    first_eval = model(inputs)
    second_eval = model(inputs)

    assert torch.equal(first_train, second_train)
    assert torch.equal(first_eval, second_eval)
    assert torch.equal(first_train, first_eval)
    assert model.training is False
    for name, value in model.state_dict().items():
        assert torch.equal(value, state_before[name])


def test_config_is_validated_and_immutable() -> None:
    config = ReferenceAffineConfig()
    assert config.operation == REFERENCE_AFFINE_OPERATION
    with pytest.raises(FrozenInstanceError):
        config.scale = 3.0  # type: ignore[misc]
    with pytest.raises(ValueError, match="finite"):
        ReferenceAffineConfig(scale=float("nan"))
    with pytest.raises(ValueError, match="dtype"):
        ReferenceAffineConfig(dtype="float64")
    with pytest.raises(ValueError, match="operation"):
        ReferenceAffineConfig(operation="reference.affine.v2")
    with pytest.raises(ValueError, match="positive"):
        ReferenceAffineConfig(maximum_input_elements=0)
    with pytest.raises(ValueError, match="v1 limit"):
        ReferenceAffineConfig(maximum_input_elements=DEFAULT_MAXIMUM_INPUT_ELEMENTS + 1)


def test_bundle_config_round_trip_preserves_input_budget() -> None:
    config = ReferenceAffineConfig(maximum_input_elements=37)
    encoded = reference_affine_config_bytes(config)

    restored = parse_reference_affine_config(encoded)

    assert restored.maximum_input_elements == 37
    assert reference_affine_config_document(restored) == reference_affine_config_document(config)


@pytest.mark.parametrize(
    "encoded, message",
    [
        (
            b'{"architecture":"reference-affine-v1","architecture":"reference-affine-v1"}',
            "unique-key",
        ),
        (b"{}", "fields"),
        (
            json.dumps(
                {
                    "architecture": "reference-affine-v1",
                    "dtype": "float32",
                    "maximum_input_elements": True,
                    "operation": "reference.affine.v1",
                    "schema_version": 1,
                }
            ).encode(),
            "integer",
        ),
    ],
)
def test_bundle_config_rejects_noncanonical_contracts(encoded: bytes, message: str) -> None:
    with pytest.raises(ValueError, match=message):
        parse_reference_affine_config(encoded)


def test_bundle_config_rejects_noncanonical_json_bytes() -> None:
    encoded = json.dumps(reference_affine_config_document(ReferenceAffineConfig())).encode()

    with pytest.raises(ValueError, match="canonical JSON"):
        parse_reference_affine_config(encoded)


def test_bundle_config_rejects_excessive_nesting_before_runtime_decode() -> None:
    encoded = b"[" * 129 + b"0" + b"]" * 129

    with pytest.raises(ValueError, match="unique-key"):
        parse_reference_affine_config(encoded)


def test_strict_state_dict_round_trip_uses_fresh_parameter_storage() -> None:
    source = ReferenceAffine(ReferenceAffineConfig(scale=3.0, bias=-1.25))
    restored = ReferenceAffine()

    incompatible = restored.load_state_dict(source.state_dict(), strict=True)

    assert incompatible.missing_keys == []
    assert incompatible.unexpected_keys == []
    assert source.scale.data_ptr() != restored.scale.data_ptr()
    assert source.bias.data_ptr() != restored.bias.data_ptr()
    inputs = torch.tensor([[-2.0, 5.0]], dtype=torch.float32)
    torch.testing.assert_close(restored(inputs), source(inputs))


def test_strict_state_dict_rejects_missing_and_unexpected_keys() -> None:
    model = ReferenceAffine()
    with pytest.raises(RuntimeError):
        model.load_state_dict({"scale": torch.tensor(1.0)}, strict=True)
    with pytest.raises(RuntimeError):
        model.load_state_dict(
            {
                "scale": torch.tensor(1.0),
                "bias": torch.tensor(0.0),
                "unexpected": torch.tensor(0.0),
            },
            strict=True,
        )


def test_safetensors_round_trip_loads_a_fresh_object(tmp_path: Path) -> None:
    source = ReferenceAffine(ReferenceAffineConfig(scale=-0.75, bias=4.0))
    checkpoint = tmp_path / "reference.safetensors"

    saved_path = save_reference_affine(source, checkpoint)
    restored = load_reference_affine(checkpoint)
    serialized = load_file(checkpoint, device="cpu")

    assert saved_path == checkpoint
    assert set(serialized) == {"scale", "bias"}
    assert all(tensor.shape == torch.Size([]) for tensor in serialized.values())
    assert all(tensor.dtype == torch.float32 for tensor in serialized.values())
    assert source is not restored
    assert source.scale.data_ptr() != restored.scale.data_ptr()
    inputs = torch.tensor([[0.0, 1.0, -2.0]], dtype=torch.float32)
    torch.testing.assert_close(restored(inputs), source(inputs))


def test_safetensors_loader_rejects_nonconforming_state(tmp_path: Path) -> None:
    checkpoint = tmp_path / "invalid.safetensors"
    save_file(
        {
            "scale": torch.tensor(2.0, dtype=torch.float64),
            "bias": torch.tensor(0.5, dtype=torch.float32),
        },
        checkpoint,
    )

    with pytest.raises(ValueError, match=r"torch\.float32"):
        load_reference_affine(checkpoint)


def test_safetensors_loader_rejects_nonfinite_state(tmp_path: Path) -> None:
    checkpoint = tmp_path / "nonfinite.safetensors"
    save_file(
        {
            "scale": torch.tensor(float("nan"), dtype=torch.float32),
            "bias": torch.tensor(0.5, dtype=torch.float32),
        },
        checkpoint,
    )

    with pytest.raises(ValueError, match="finite"):
        load_reference_affine(checkpoint)


def test_safetensors_loader_rejects_symlink(tmp_path: Path) -> None:
    target = tmp_path / "target.safetensors"
    checkpoint = tmp_path / "link.safetensors"
    save_file(
        {
            "scale": torch.tensor(2.0, dtype=torch.float32),
            "bias": torch.tensor(0.5, dtype=torch.float32),
        },
        target,
    )
    checkpoint.symlink_to(target)

    with pytest.raises(ValueError, match="opened safely"):
        load_reference_affine(checkpoint)
