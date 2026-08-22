# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Strict residual addition without implicit broadcasting."""

from __future__ import annotations

import math

import torch
from torch import nn


def validate_residual_scale(scale: float) -> float:
    if isinstance(scale, bool) or not isinstance(scale, int | float):
        raise TypeError("residual scale must be a real number")
    resolved = float(scale)
    if not math.isfinite(resolved):
        raise ValueError("residual scale must be finite")
    return resolved


def residual_add(
    residual: torch.Tensor,
    update: torch.Tensor,
    *,
    scale: float = 1.0,
) -> torch.Tensor:
    """Return ``residual + scale * update`` for identical tensor contracts."""

    if not isinstance(residual, torch.Tensor) or not isinstance(update, torch.Tensor):
        raise TypeError("residual and update must be torch.Tensor values")
    if residual.layout is not torch.strided or update.layout is not torch.strided:
        raise TypeError("residual and update must use strided layouts")
    if not residual.is_floating_point() or not update.is_floating_point():
        raise TypeError("residual and update must have floating-point dtypes")
    if residual.shape != update.shape:
        raise ValueError("residual and update must have identical shapes")
    if residual.numel() == 0:
        raise ValueError("residual and update must be nonempty")
    if residual.dtype != update.dtype:
        raise TypeError("residual and update must have the same dtype")
    if residual.device != update.device:
        raise ValueError("residual and update must be on the same device")
    return torch.add(residual, update, alpha=validate_residual_scale(scale))


class ResidualAdd(nn.Module):
    """Stateless module form of :func:`residual_add`."""

    scale: float

    def __init__(self, scale: float = 1.0) -> None:
        super().__init__()
        self.scale = validate_residual_scale(scale)

    def forward(self, residual: torch.Tensor, update: torch.Tensor) -> torch.Tensor:
        return residual_add(residual, update, scale=self.scale)

    def extra_repr(self) -> str:
        return f"scale={self.scale}"
