# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import torch

from kernels.ops.fp8.reference import scaled_gemm_reference


def scaled_linear_reference(
    inputs: torch.Tensor,
    weight: torch.Tensor,
    input_scale: torch.Tensor,
    weight_scale: torch.Tensor,
    bias: torch.Tensor | None = None,
    *,
    output_dtype: torch.dtype = torch.bfloat16,
) -> torch.Tensor:
    output = scaled_gemm_reference(
        inputs,
        weight.transpose(0, 1),
        input_scale,
        weight_scale,
        output_dtype=torch.float32,
    )
    if bias is not None:
        if bias.shape != (weight.shape[0],):
            raise ValueError("bias must match the linear output dimension")
        output = output + bias.float()
    return output.to(output_dtype)
