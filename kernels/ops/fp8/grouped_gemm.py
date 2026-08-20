# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import torch


def grouped_scaled_gemm_reference(
    inputs: torch.Tensor,
    weights: torch.Tensor,
    input_scales: torch.Tensor,
    weight_scales: torch.Tensor,
    *,
    output_dtype: torch.dtype = torch.bfloat16,
) -> torch.Tensor:
    if inputs.ndim != 3 or weights.ndim != 3:
        raise ValueError("grouped GEMM inputs and weights must have rank three")
    if inputs.shape[0] != weights.shape[0] or inputs.shape[2] != weights.shape[1]:
        raise ValueError("grouped GEMM expert and reduction dimensions must agree")
    experts = inputs.shape[0]
    if input_scales.shape != (experts, 1, 1) or weight_scales.shape != (experts, 1, 1):
        raise ValueError("grouped GEMM scales must have shape [experts, 1, 1]")
    output = torch.bmm(inputs.float(), weights.float())
    return (output * input_scales.float() * weight_scales.float()).to(output_dtype)
