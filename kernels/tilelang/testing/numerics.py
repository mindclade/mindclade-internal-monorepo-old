# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Numerical parity helpers with fixed, reviewable dtype policies."""

from __future__ import annotations

from dataclasses import dataclass

import torch


@dataclass(frozen=True, slots=True)
class Tolerance:
    rtol: float
    atol: float

    def __post_init__(self) -> None:
        if self.rtol < 0 or self.atol < 0:
            raise ValueError("tolerances must be non-negative")


TOLERANCES: dict[torch.dtype, Tolerance] = {
    torch.float64: Tolerance(1e-8, 1e-10),
    torch.float32: Tolerance(2e-5, 2e-6),
    torch.float16: Tolerance(2e-2, 2e-2),
    torch.bfloat16: Tolerance(3e-2, 3e-2),
    torch.float8_e4m3fn: Tolerance(1.5e-1, 1.5e-1),
    torch.float8_e5m2: Tolerance(2.5e-1, 2.5e-1),
}


@dataclass(frozen=True, slots=True)
class ParityReport:
    passed: bool
    max_absolute_error: float
    max_relative_error: float
    mismatched: int
    elements: int


def parity_report(
    actual: torch.Tensor,
    expected: torch.Tensor,
    *,
    tolerance: Tolerance | None = None,
    equal_nan: bool = False,
) -> ParityReport:
    if actual.shape != expected.shape:
        raise ValueError("parity tensors must have identical shapes")
    policy = TOLERANCES.get(expected.dtype) if tolerance is None else tolerance
    if policy is None:
        raise ValueError(f"no tolerance policy for dtype {expected.dtype}")
    actual_f = actual.double()
    expected_f = expected.double()
    absolute = (actual_f - expected_f).abs()
    denominator = expected_f.abs().clamp_min(torch.finfo(torch.float64).tiny)
    relative = absolute / denominator
    close = torch.isclose(
        actual_f, expected_f, rtol=policy.rtol, atol=policy.atol, equal_nan=equal_nan
    )
    elements = expected.numel()
    return ParityReport(
        passed=bool(close.all()),
        max_absolute_error=float(absolute.max()) if elements else 0.0,
        max_relative_error=float(relative.max()) if elements else 0.0,
        mismatched=int((~close).sum()),
        elements=elements,
    )


def assert_parity(
    actual: torch.Tensor,
    expected: torch.Tensor,
    *,
    tolerance: Tolerance | None = None,
    equal_nan: bool = False,
) -> None:
    report = parity_report(actual, expected, tolerance=tolerance, equal_nan=equal_nan)
    if not report.passed:
        raise AssertionError(
            "numerical parity failed: "
            f"mismatched={report.mismatched}/{report.elements}, "
            f"max_abs={report.max_absolute_error:.6g}, "
            f"max_rel={report.max_relative_error:.6g}"
        )
