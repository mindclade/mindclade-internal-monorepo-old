# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Explicit FP8 scaling contract."""

from __future__ import annotations

from dataclasses import dataclass

import torch

from kernels.ops.fp8.formats import FP8Format


@dataclass(frozen=True, slots=True)
class QuantizedTensor:
    values: torch.Tensor
    scale: torch.Tensor
    format: FP8Format

    def dequantize(self) -> torch.Tensor:
        return self.values.float() * self.scale.float()


def per_tensor_scale(tensor: torch.Tensor, format: FP8Format) -> torch.Tensor:
    if not tensor.is_floating_point() or tensor.numel() == 0:
        raise ValueError("FP8 scaling requires a non-empty floating-point tensor")
    amax = tensor.detach().abs().float().amax()
    one = torch.ones((), device=tensor.device, dtype=torch.float32)
    return torch.where(amax == 0, one, amax / format.max_finite).reshape(1)
