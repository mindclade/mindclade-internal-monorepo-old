# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

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
