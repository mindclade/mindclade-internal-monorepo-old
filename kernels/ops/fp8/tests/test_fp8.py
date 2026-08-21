# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest
import torch

from kernels.ops.fp8 import FP8Format, quantize_per_tensor, scaled_gemm_reference


@pytest.mark.parametrize("format", list(FP8Format))
def test_per_tensor_quantization_is_finite_saturating_and_deterministic(format: FP8Format) -> None:
    values = torch.tensor([-1000.0, -1.0, 0.0, 1.0, 1000.0])
    first = quantize_per_tensor(values, format)
    second = quantize_per_tensor(values, format)
    assert torch.equal(first.values, second.values)
    assert torch.equal(first.scale, second.scale)
    assert torch.isfinite(first.dequantize()).all()
    torch.testing.assert_close(
        first.dequantize(), values, rtol=0.13 if format == FP8Format.E4M3FN else 0.26, atol=0.1
    )


def test_scaled_gemm_matches_decomposed_fp32_reference_and_epilogue() -> None:
    torch.manual_seed(9)
    a = torch.randn(5, 7)
    b = torch.randn(7, 3)
    a_scale = torch.tensor([0.25])
    b_scale = torch.tensor([2.0])
    actual = scaled_gemm_reference(
        a, b, a_scale, b_scale, output_dtype=torch.float32, activation="silu"
    )
    expected = F_silu((a @ b) * 0.5)
    torch.testing.assert_close(actual, expected)


def F_silu(value: torch.Tensor) -> torch.Tensor:
    return value * torch.sigmoid(value)


def test_fp8_admission_rejects_nonfinite_values_and_bad_scales() -> None:
    with pytest.raises(ValueError, match="NaN and infinity"):
        quantize_per_tensor(torch.tensor([float("nan")]))
    with pytest.raises(RuntimeError, match="strictly positive"):
        scaled_gemm_reference(
            torch.ones(1, 1), torch.ones(1, 1), torch.tensor([0.0]), torch.tensor([1.0])
        )
