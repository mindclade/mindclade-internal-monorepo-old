# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Connected-H100 synchronized TileLang latency and memory regression gates."""

from __future__ import annotations

import os
from collections.abc import Callable
from functools import partial

import pytest
import torch

from kernels.api.capabilities import DeviceCapabilities
from kernels.tilelang.testing.connected import (
    connected_cases,
    connected_device,
    implementations_for,
)
from kernels.tilelang.testing.numerics import assert_parity
from kernels.tilelang.testing.performance import benchmark_callable


def _require_hopper() -> tuple[torch.device, DeviceCapabilities]:
    expected_target = os.environ.get("MINDCLADE_EXPECTED_GPU_TARGET")
    expected_architecture = os.environ.get("MINDCLADE_EXPECTED_GPU_ARCHITECTURE")
    if expected_target is None and expected_architecture is None:
        pytest.skip("performance gates run only in the explicit accelerator qualification job")
    if expected_target != "cuda" or expected_architecture != "sm_90":
        pytest.fail(
            "connected performance requires exactly "
            "MINDCLADE_EXPECTED_GPU_TARGET=cuda and "
            "MINDCLADE_EXPECTED_GPU_ARCHITECTURE=sm_90"
        )
    try:
        device, capabilities = connected_device()
    except RuntimeError as error:
        pytest.fail(f"explicit cuda/sm_90 performance device is unavailable: {error}")
    if capabilities.target != expected_target or capabilities.architecture != expected_architecture:
        pytest.fail(
            "connected performance device does not match the explicit cuda/sm_90 contract: "
            f"found {capabilities.target}/{capabilities.architecture}"
        )
    return device, capabilities


def _incremental_peak_bytes(function: Callable[[], object]) -> int:
    torch.cuda.synchronize()
    torch.cuda.reset_peak_memory_stats()
    baseline = torch.cuda.memory_allocated()
    function()
    torch.cuda.synchronize()
    return max(1, torch.cuda.max_memory_allocated() - baseline)


def test_tilelang_latency_variance_tail_and_memory_regression() -> None:
    device, capabilities = _require_hopper()
    for case in connected_cases(device):
        candidate, reference = implementations_for(case, capabilities)
        expected = reference.invoke(*case.arguments, **case.keywords)
        actual = candidate.invoke(*case.arguments)
        assert_parity(actual, expected)
        torch.cuda.synchronize()

        candidate_call = partial(candidate.invoke, *case.arguments)
        baseline_call = partial(reference.invoke, *case.arguments, **case.keywords)
        candidate_result = benchmark_callable(
            candidate_call,
            operation=case.request.operation,
            request_digest=case.request.digest,
            implementation_digest=candidate.identity.digest,
            environment_digest=capabilities.runtime_environment_digest,
            synchronize=torch.cuda.synchronize,
            correctness_passed=True,
            warmup=10,
            repeats=50,
        )
        baseline_result = benchmark_callable(
            baseline_call,
            operation=case.request.operation,
            request_digest=case.request.digest,
            implementation_digest=reference.identity.digest,
            environment_digest=capabilities.runtime_environment_digest,
            synchronize=torch.cuda.synchronize,
            correctness_passed=True,
            warmup=10,
            repeats=50,
        )
        candidate_latency = candidate_result.latency
        baseline_latency = baseline_result.latency
        assert baseline_latency.median_ms / candidate_latency.median_ms >= 1.05, case.name
        assert candidate_latency.relative_mad <= 0.05, case.name
        assert candidate_latency.p95_ms <= baseline_latency.p95_ms, case.name
        assert _incremental_peak_bytes(candidate_call) <= _incremental_peak_bytes(baseline_call), (
            case.name
        )
