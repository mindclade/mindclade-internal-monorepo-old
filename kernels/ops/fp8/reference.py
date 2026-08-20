# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Decomposed FP32 reference for scaled low-precision GEMM."""

from __future__ import annotations

import torch

from kernels.ops.fp8.validation import validate_scaled_gemm


def scaled_gemm_reference(
    a: torch.Tensor,
    b: torch.Tensor,
    a_scale: torch.Tensor,
    b_scale: torch.Tensor,
    *,
    output_dtype: torch.dtype = torch.bfloat16,
    activation: str = "none",
) -> torch.Tensor:
    validate_scaled_gemm(a, b, a_scale, b_scale)
    if activation not in {"none", "relu", "silu"}:
        raise ValueError("activation must be none, relu, or silu")
    output = torch.matmul(a.float(), b.float()) * a_scale.float() * b_scale.float()
    if activation == "relu":
        output = torch.relu(output)
    elif activation == "silu":
        output = torch.nn.functional.silu(output)
    return output.to(output_dtype)
