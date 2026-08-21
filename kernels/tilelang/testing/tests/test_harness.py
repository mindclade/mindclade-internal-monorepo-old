# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest
import torch

from kernels.tilelang.testing.numerics import Tolerance, assert_parity, parity_report
from kernels.tilelang.testing.performance import benchmark_callable


def test_parity_report_uses_fixed_dtype_policy() -> None:
    expected = torch.tensor([1.0, -2.0], dtype=torch.float32)
    actual = expected + torch.tensor([1e-6, -1e-6])
    report = parity_report(actual, expected)
    assert report.passed
    assert report.elements == 2
    assert report.mismatched == 0


def test_parity_failure_has_reproducible_error_summary() -> None:
    expected = torch.zeros(3)
    actual = torch.ones(3)
    with pytest.raises(AssertionError, match="mismatched=3/3"):
        assert_parity(actual, expected, tolerance=Tolerance(0, 0))


def test_parity_rejects_dtype_and_tolerance_spoofs() -> None:
    with pytest.raises(TypeError, match="identical dtypes"):
        parity_report(torch.ones(2, dtype=torch.float16), torch.ones(2, dtype=torch.float32))
    with pytest.raises(ValueError, match="finite"):
        Tolerance(float("nan"), 0.0)
    with pytest.raises(TypeError, match="real number"):
        Tolerance(True, 0.0)


def test_benchmark_callable_synchronizes_and_records_distribution() -> None:
    calls: list[str] = []

    result = benchmark_callable(
        lambda: calls.append("kernel"),
        operation="test.operation",
        request_digest="a" * 64,
        implementation_digest="b" * 64,
        environment_digest="c" * 64,
        synchronize=lambda: calls.append("sync"),
        correctness_passed=True,
        warmup=2,
        repeats=5,
    )

    assert len(result.latency.samples_ms) == 5
    assert result.latency.median_ms >= 0
    assert calls.count("kernel") == 7
    assert calls.count("sync") == 11


def test_benchmark_callable_requires_correctness() -> None:
    with pytest.raises(ValueError, match="forbidden"):
        benchmark_callable(
            lambda: None,
            operation="test.operation",
            request_digest="a" * 64,
            implementation_digest="b" * 64,
            environment_digest="c" * 64,
            synchronize=lambda: None,
            correctness_passed=False,
        )


def test_benchmark_rejects_identity_and_boolean_count_spoofs_before_execution() -> None:
    calls = 0

    def invoke() -> None:
        nonlocal calls
        calls += 1

    with pytest.raises(ValueError, match="request_digest"):
        benchmark_callable(
            invoke,
            operation="test.operation",
            request_digest="mutable",
            implementation_digest="b" * 64,
            environment_digest="c" * 64,
            synchronize=lambda: None,
            correctness_passed=True,
        )
    with pytest.raises(ValueError, match="warmup"):
        benchmark_callable(
            invoke,
            operation="test.operation",
            request_digest="a" * 64,
            implementation_digest="b" * 64,
            environment_digest="c" * 64,
            synchronize=lambda: None,
            correctness_passed=True,
            warmup=True,
        )
    assert calls == 0
