# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Connected-H100 TileLang parity, determinism, and fallback-gradient gates."""

from __future__ import annotations

import os

import pytest
import torch

from kernels.api.capabilities import DeviceCapabilities
from kernels.qualification.workloads import production_workload_pairs
from kernels.registry import KernelImplementation
from kernels.tilelang.testing.connected import (
    ConnectedCase,
    catalog_capabilities_for,
    connected_device,
    implementations_for,
    implementations_for_request,
    production_connected_cases,
    validate_tensor_argument,
)
from kernels.tilelang.testing.numerics import assert_parity


def _require_hopper() -> tuple[torch.device, DeviceCapabilities]:
    expected_target = os.environ.get("MINDCLADE_EXPECTED_GPU_TARGET")
    expected_architecture = os.environ.get("MINDCLADE_EXPECTED_GPU_ARCHITECTURE")
    if expected_target is None and expected_architecture is None:
        pytest.skip("connected parity runs only in the explicit accelerator qualification job")
    if expected_target != "cuda" or expected_architecture != "sm_90":
        pytest.fail(
            "connected parity requires exactly "
            "MINDCLADE_EXPECTED_GPU_TARGET=cuda and "
            "MINDCLADE_EXPECTED_GPU_ARCHITECTURE=sm_90"
        )
    try:
        device, capabilities = connected_device()
    except RuntimeError as error:
        pytest.fail(f"explicit cuda/sm_90 qualification device is unavailable: {error}")
    if capabilities.target != expected_target or capabilities.architecture != expected_architecture:
        pytest.fail(
            "connected device does not match the explicit cuda/sm_90 qualification contract: "
            f"found {capabilities.target}/{capabilities.architecture}"
        )
    return device, capabilities


def _assert_reference_gradients(
    case: ConnectedCase,
    reference: KernelImplementation,
    gradient_inputs: tuple[int, ...],
) -> None:
    arguments = tuple(
        argument.detach().clone().requires_grad_(index in gradient_inputs)
        if argument.is_floating_point()
        else argument
        for index, argument in enumerate(case.arguments)
    )
    output = reference.invoke(*arguments, **case.keywords)
    assert isinstance(output, torch.Tensor), case.name
    torch.autograd.backward(output.float().sum())
    for index in gradient_inputs:
        gradient = arguments[index].grad
        assert gradient is not None, case.name
        # CPU torch.isfinite does not implement Float8 directly. FP32 conversion
        # preserves the finite/non-finite classification for every supported dtype.
        assert torch.isfinite(gradient.float()).all(), case.name


@pytest.mark.parametrize(
    ("target", "architecture"),
    [
        (None, "sm_90"),
        ("cuda", None),
        ("hip", "sm_90"),
        ("cuda", "sm_80"),
    ],
)
def test_partial_or_wrong_explicit_connected_contract_fails(
    monkeypatch: pytest.MonkeyPatch,
    target: str | None,
    architecture: str | None,
) -> None:
    for name, value in (
        ("MINDCLADE_EXPECTED_GPU_TARGET", target),
        ("MINDCLADE_EXPECTED_GPU_ARCHITECTURE", architecture),
    ):
        if value is None:
            monkeypatch.delenv(name, raising=False)
        else:
            monkeypatch.setenv(name, value)
    with pytest.raises(pytest.fail.Exception, match="requires exactly"):
        _require_hopper()


def test_explicit_hopper_contract_fails_when_cuda_is_unavailable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("MINDCLADE_EXPECTED_GPU_TARGET", "cuda")
    monkeypatch.setenv("MINDCLADE_EXPECTED_GPU_ARCHITECTURE", "sm_90")
    monkeypatch.setattr(torch.cuda, "is_available", lambda: False)
    with pytest.raises(pytest.fail.Exception, match="device is unavailable"):
        _require_hopper()


def test_all_production_requests_map_to_deterministic_exact_cpu_cases() -> None:
    pairs = production_workload_pairs()
    device = torch.device("cpu")
    names: set[str] = set()
    mapped = 0
    first_cases = production_connected_cases(device, pairs)
    repeated_cases = production_connected_cases(device, pairs)

    for pair, case, repeated in zip(pairs, first_cases, repeated_cases, strict=True):
        assert case.request is pair.inference
        assert case.paired_training_request is pair.training
        assert case.name not in names
        names.add(case.name)
        assert len(case.arguments) == len(pair.inference.inputs)
        for index, (argument, repeated_argument, specification) in enumerate(
            zip(case.arguments, repeated.arguments, pair.inference.inputs, strict=True)
        ):
            assert argument.device == device
            validate_tensor_argument(argument, specification, input_index=index)
            torch.testing.assert_close(argument, repeated_argument, rtol=0, atol=0)
        mapped += 1

    assert mapped == 124
    assert len(names) == 124


def test_all_eligible_requests_resolve_uniquely_and_training_falls_back_on_cpu() -> None:
    pairs = production_workload_pairs()
    for pair in pairs:
        capabilities = catalog_capabilities_for(pair.inference)
        candidate, reference = implementations_for_request(pair.inference, capabilities)
        assert candidate.rejection_reason(pair.inference, capabilities) is None
        assert reference.rejection_reason(pair.inference, capabilities) is None
        assert candidate.rejection_reason(pair.training, capabilities) == "execution_mode"
        assert reference.rejection_reason(pair.training, capabilities) is None
        assert tuple(sorted(reference.differentiable_inputs)) == pair.training.gradient_inputs


def test_all_training_fallback_references_have_finite_cpu_gradients() -> None:
    pairs = production_workload_pairs()
    executed = 0
    for pair, case in zip(
        pairs,
        production_connected_cases(torch.device("cpu"), pairs),
        strict=True,
    ):
        _, reference = implementations_for_request(
            pair.inference,
            catalog_capabilities_for(pair.inference),
        )
        _assert_reference_gradients(case, reference, pair.training.gradient_inputs)
        executed += 1
    assert executed == 124


def test_all_production_tilelang_inference_requests_have_h100_parity() -> None:
    device, capabilities = _require_hopper()
    pairs = production_workload_pairs()
    executed = 0
    for pair, case in zip(
        pairs,
        production_connected_cases(device, pairs),
        strict=True,
    ):
        assert case.request is pair.inference
        candidate, reference = implementations_for(case, capabilities)
        expected = reference.invoke(*case.arguments, **case.keywords)
        actual = candidate.invoke(*case.arguments)
        repeated = candidate.invoke(*case.arguments)
        assert torch.isfinite(actual).all(), case.name
        assert_parity(actual, expected)
        torch.testing.assert_close(actual, repeated, rtol=0, atol=0)
        executed += 1
    assert executed == 124


def test_all_production_training_requests_fall_back_with_finite_reference_gradients() -> None:
    device, capabilities = _require_hopper()
    pairs = production_workload_pairs()
    executed = 0
    for pair, case in zip(
        pairs,
        production_connected_cases(device, pairs),
        strict=True,
    ):
        candidate, reference = implementations_for(case, capabilities)
        training = case.paired_training_request
        assert training is pair.training
        gradient_inputs = training.gradient_inputs
        assert gradient_inputs == tuple(sorted(reference.differentiable_inputs))
        assert candidate.rejection_reason(training, capabilities) == "execution_mode"
        assert reference.rejection_reason(training, capabilities) is None
        _assert_reference_gradients(case, reference, gradient_inputs)
        executed += 1
    assert executed == 124
