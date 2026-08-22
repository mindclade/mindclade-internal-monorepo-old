# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""RMS normalization with explicit shape, dtype, and placement contracts."""

from __future__ import annotations

import math
from collections.abc import Sequence

import torch
from torch import nn

MAXIMUM_NORMALIZED_RANK = 8
MAXIMUM_NORMALIZED_ELEMENTS = 16_777_216
MAXIMUM_INPUT_RANK = 16


def canonical_normalized_shape(normalized_shape: int | Sequence[int]) -> tuple[int, ...]:
    """Validate and canonicalize a trailing normalized shape."""

    if isinstance(normalized_shape, bool):
        raise TypeError("normalized_shape must contain integers")
    if isinstance(normalized_shape, int):
        values: tuple[int, ...] = (normalized_shape,)
    elif isinstance(normalized_shape, Sequence) and not isinstance(normalized_shape, str | bytes):
        values = tuple(normalized_shape)
    else:
        raise TypeError("normalized_shape must be an integer or a sequence of integers")

    if not values or len(values) > MAXIMUM_NORMALIZED_RANK:
        raise ValueError("normalized_shape rank is outside bounds")
    if any(isinstance(value, bool) or not isinstance(value, int) for value in values):
        raise TypeError("normalized_shape must contain integers")
    if any(value <= 0 for value in values):
        raise ValueError("normalized_shape dimensions must be positive")
    if math.prod(values) > MAXIMUM_NORMALIZED_ELEMENTS:
        raise ValueError("normalized_shape exceeds the parameter element bound")
    return values


def validate_epsilon(eps: float) -> float:
    if isinstance(eps, bool) or not isinstance(eps, int | float):
        raise TypeError("eps must be a real number")
    resolved = float(eps)
    if not math.isfinite(resolved) or resolved <= 0.0:
        raise ValueError("eps must be finite and positive")
    return resolved


def validate_normalization_input(
    inputs: torch.Tensor,
    normalized_shape: tuple[int, ...],
    *,
    operation: str,
    parameters: tuple[nn.Parameter | None, ...] = (),
) -> None:
    """Fail near the module boundary without synchronizing tensor values."""

    if not isinstance(inputs, torch.Tensor):
        raise TypeError(f"{operation} input must be a torch.Tensor")
    if inputs.layout is not torch.strided:
        raise TypeError(f"{operation} input must use a strided layout")
    if not inputs.is_floating_point():
        raise TypeError(f"{operation} input must have a floating-point dtype")
    if inputs.ndim < len(normalized_shape) or inputs.ndim > MAXIMUM_INPUT_RANK:
        raise ValueError(f"{operation} input rank is outside bounds")
    if inputs.numel() == 0:
        raise ValueError(f"{operation} input must be nonempty")
    if tuple(inputs.shape[-len(normalized_shape) :]) != normalized_shape:
        raise ValueError(f"{operation} input trailing dimensions must match normalized_shape")

    for parameter in parameters:
        if parameter is None:
            continue
        if parameter.device != inputs.device:
            raise ValueError(f"{operation} input and parameters must be on the same device")
        if parameter.dtype != inputs.dtype:
            raise TypeError(f"{operation} input and parameters must have the same dtype")


class RMSNorm(nn.Module):
    """Normalize over trailing dimensions using an FP32-or-better mean square.

    Input is a nonempty dense strided floating tensor ``[..., *normalized_shape]``.
    Output preserves its shape, dtype, and device. The caller owns placement and
    dtype conversion. NaN and infinity propagation follows ordinary PyTorch
    arithmetic and is intentionally not hidden behind a device synchronization.
    """

    normalized_shape: tuple[int, ...]
    eps: float
    elementwise_affine: bool
    weight: nn.Parameter | None

    def __init__(
        self,
        normalized_shape: int | Sequence[int],
        eps: float = 1e-6,
        *,
        elementwise_affine: bool = True,
        device: torch.device | str | None = None,
        dtype: torch.dtype | None = None,
    ) -> None:
        super().__init__()
        self.normalized_shape = canonical_normalized_shape(normalized_shape)
        self.eps = validate_epsilon(eps)
        if not isinstance(elementwise_affine, bool):
            raise TypeError("elementwise_affine must be a boolean")
        self.elementwise_affine = elementwise_affine
        if elementwise_affine:
            self.weight = nn.Parameter(
                torch.ones(self.normalized_shape, device=device, dtype=dtype)
            )
        else:
            self.register_parameter("weight", None)

    def forward(self, inputs: torch.Tensor) -> torch.Tensor:
        validate_normalization_input(
            inputs,
            self.normalized_shape,
            operation="RMSNorm",
            parameters=(self.weight,),
        )
        reduction_dtype = torch.float64 if inputs.dtype is torch.float64 else torch.float32
        promoted = inputs.to(dtype=reduction_dtype)
        dimensions = tuple(range(inputs.ndim - len(self.normalized_shape), inputs.ndim))
        inverse_rms = torch.rsqrt(promoted.square().mean(dim=dimensions, keepdim=True) + self.eps)
        output = (promoted * inverse_rms).to(dtype=inputs.dtype)
        if self.weight is not None:
            output = output * self.weight
        return output

    def reset_parameters(self) -> None:
        if self.weight is not None:
            nn.init.ones_(self.weight)

    def extra_repr(self) -> str:
        return (
            f"normalized_shape={self.normalized_shape}, eps={self.eps}, "
            f"elementwise_affine={self.elementwise_affine}"
        )
