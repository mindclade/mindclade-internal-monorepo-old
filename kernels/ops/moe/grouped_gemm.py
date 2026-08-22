# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import torch

from kernels.ops.moe.validation import validate_grouped_gemm


def padded_grouped_gemm_reference(
    inputs: torch.Tensor, weights: torch.Tensor, *, output_dtype: torch.dtype | None = None
) -> torch.Tensor:
    validate_grouped_gemm(inputs, weights)
    dtype = inputs.dtype if output_dtype is None else output_dtype
    return torch.bmm(inputs.float(), weights.float()).to(dtype)
