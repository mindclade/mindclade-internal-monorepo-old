# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Layer normalization with stable state keys and strict input validation."""

from __future__ import annotations

from collections.abc import Sequence

import torch
from torch import nn
from torch.nn import functional as F

from .rms_norm import (
    canonical_normalized_shape,
    validate_epsilon,
    validate_normalization_input,
)


class LayerNorm(nn.Module):
    """Apply PyTorch layer-normalization semantics over trailing dimensions."""

    normalized_shape: tuple[int, ...]
    eps: float
    elementwise_affine: bool
    use_bias: bool
    weight: nn.Parameter | None
    bias: nn.Parameter | None

    def __init__(
        self,
        normalized_shape: int | Sequence[int],
        eps: float = 1e-5,
        *,
        elementwise_affine: bool = True,
        bias: bool = True,
        device: torch.device | str | None = None,
        dtype: torch.dtype | None = None,
    ) -> None:
        super().__init__()
        self.normalized_shape = canonical_normalized_shape(normalized_shape)
        self.eps = validate_epsilon(eps)
        if not isinstance(elementwise_affine, bool) or not isinstance(bias, bool):
            raise TypeError("elementwise_affine and bias must be booleans")
        self.elementwise_affine = elementwise_affine
        self.use_bias = bias and elementwise_affine

        if elementwise_affine:
            self.weight = nn.Parameter(
                torch.ones(self.normalized_shape, device=device, dtype=dtype)
            )
            if bias:
                self.bias = nn.Parameter(
                    torch.zeros(self.normalized_shape, device=device, dtype=dtype)
                )
            else:
                self.register_parameter("bias", None)
        else:
            self.register_parameter("weight", None)
            self.register_parameter("bias", None)

    def forward(self, inputs: torch.Tensor) -> torch.Tensor:
        validate_normalization_input(
            inputs,
            self.normalized_shape,
            operation="LayerNorm",
            parameters=(self.weight, self.bias),
        )
        return F.layer_norm(inputs, self.normalized_shape, self.weight, self.bias, self.eps)

    def reset_parameters(self) -> None:
        if self.weight is not None:
            nn.init.ones_(self.weight)
        if self.bias is not None:
            nn.init.zeros_(self.bias)

    def extra_repr(self) -> str:
        return (
            f"normalized_shape={self.normalized_shape}, eps={self.eps}, "
            f"elementwise_affine={self.elementwise_affine}, bias={self.use_bias}"
        )
