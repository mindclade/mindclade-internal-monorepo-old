# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model-owned activation modules backed by canonical kernel semantics."""

from __future__ import annotations

import torch
from torch import nn

from kernels.ops.fused import swiglu_reference


def swiglu(gate: torch.Tensor, up: torch.Tensor) -> torch.Tensor:
    """Apply FP32-or-better SiLU to ``gate`` and multiply by ``up``.

    Inputs must be nonempty floating tensors with identical shape, dtype, and
    device. Output preserves that contract. Qualified accelerator providers
    must demonstrate parity with this eager semantic reference.
    """

    if not isinstance(gate, torch.Tensor) or not isinstance(up, torch.Tensor):
        raise TypeError("SwiGLU inputs must be torch.Tensor values")
    if gate.layout is not torch.strided or up.layout is not torch.strided:
        raise TypeError("SwiGLU inputs must use strided layouts")
    if gate.shape != up.shape:
        raise ValueError("SwiGLU inputs must have identical shapes")
    if gate.numel() == 0:
        raise ValueError("SwiGLU inputs must be nonempty")
    if not gate.is_floating_point() or not up.is_floating_point():
        raise TypeError("SwiGLU inputs must have floating-point dtypes")
    if gate.dtype != up.dtype:
        raise TypeError("SwiGLU inputs must have the same dtype")
    if gate.device != up.device:
        raise ValueError("SwiGLU inputs must be on the same device")
    return swiglu_reference(gate, up)


class SwiGLU(nn.Module):
    """Stateless module form of :func:`swiglu`."""

    def forward(self, gate: torch.Tensor, up: torch.Tensor) -> torch.Tensor:
        return swiglu(gate, up)
