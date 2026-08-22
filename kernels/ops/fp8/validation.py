# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import torch

from kernels.api.errors import KernelValidationError
from kernels.api.validation import require_positive_dimensions, require_rank, require_same_device


def validate_scaled_gemm(
    a: torch.Tensor,
    b: torch.Tensor,
    a_scale: torch.Tensor,
    b_scale: torch.Tensor,
) -> None:
    require_rank(a, 2, "a")
    require_rank(b, 2, "b")
    require_positive_dimensions(a, "a")
    require_positive_dimensions(b, "b")
    require_same_device(("a", a), ("b", b), ("a_scale", a_scale), ("b_scale", b_scale))
    if a.shape[1] != b.shape[0]:
        raise KernelValidationError("a.shape[1] must equal b.shape[0]")
    if a_scale.numel() != 1 or b_scale.numel() != 1:
        raise KernelValidationError("scaled GEMM currently requires one-element scales")
    if not torch.isfinite(a_scale).all() or not torch.isfinite(b_scale).all():
        raise KernelValidationError("scales must be finite")
    if (a_scale <= 0).any() or (b_scale <= 0).any():
        raise KernelValidationError("scales must be strictly positive")
