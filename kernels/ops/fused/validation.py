# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import torch

from kernels.api.errors import KernelValidationError
from kernels.api.validation import require_same_device, require_same_dtype


def validate_binary(a: torch.Tensor, b: torch.Tensor, operation: str) -> None:
    require_same_device(("a", a), ("b", b))
    require_same_dtype(("a", a), ("b", b))
    if a.shape != b.shape:
        raise KernelValidationError(f"{operation} inputs must have identical shapes")
    if a.numel() == 0 or not a.is_floating_point():
        raise KernelValidationError(f"{operation} requires non-empty floating-point tensors")


def validate_triangle(left: torch.Tensor, right: torch.Tensor, mask: torch.Tensor) -> None:
    require_same_device(("left", left), ("right", right), ("mask", mask))
    require_same_dtype(("left", left), ("right", right))
    if left.ndim != 4 or left.shape != right.shape:
        raise KernelValidationError("triangle inputs must share [batch, N, N, channels] shape")
    batch, rows, columns, _ = left.shape
    if rows <= 0 or rows != columns:
        raise KernelValidationError("triangle pair dimensions must be equal and non-empty")
    if mask.shape != (batch, rows) or mask.dtype != torch.bool:
        raise KernelValidationError("triangle mask must be boolean [batch, N]")
