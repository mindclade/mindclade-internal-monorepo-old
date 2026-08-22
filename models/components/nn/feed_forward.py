# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""SwiGLU feed-forward layer with stable parameter names."""

from __future__ import annotations

import torch
from torch import nn

from .activations import SwiGLU

MAXIMUM_FEATURE_DIMENSION = 1_048_576
MAXIMUM_INPUT_RANK = 16


def _validate_feature_dimension(value: int, name: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise TypeError(f"{name} must be an integer")
    if not 1 <= value <= MAXIMUM_FEATURE_DIMENSION:
        raise ValueError(f"{name} is outside bounds")
    return value


class SwiGLUFeedForward(nn.Module):
    """Project ``[..., D]`` through a gated ``F``-wide hidden representation.

    Parameter names are exactly ``gate_proj``, ``up_proj``, and ``down_proj``.
    The caller owns dtype and device placement; forward never casts or moves
    the input. The SwiGLU nonlinearity evaluates SiLU in FP32-or-better
    precision and casts the activation back to the projection dtype before the
    output projection.
    """

    hidden_size: int
    intermediate_size: int
    use_bias: bool

    def __init__(
        self,
        hidden_size: int,
        intermediate_size: int,
        *,
        bias: bool = False,
        device: torch.device | str | None = None,
        dtype: torch.dtype | None = None,
    ) -> None:
        super().__init__()
        self.hidden_size = _validate_feature_dimension(hidden_size, "hidden_size")
        self.intermediate_size = _validate_feature_dimension(intermediate_size, "intermediate_size")
        if not isinstance(bias, bool):
            raise TypeError("bias must be a boolean")
        self.use_bias = bias
        factory_kwargs = {"device": device, "dtype": dtype}
        self.gate_proj = nn.Linear(
            self.hidden_size,
            self.intermediate_size,
            bias=bias,
            **factory_kwargs,
        )
        self.up_proj = nn.Linear(
            self.hidden_size,
            self.intermediate_size,
            bias=bias,
            **factory_kwargs,
        )
        self.activation = SwiGLU()
        self.down_proj = nn.Linear(
            self.intermediate_size,
            self.hidden_size,
            bias=bias,
            **factory_kwargs,
        )

    def forward(self, inputs: torch.Tensor) -> torch.Tensor:
        self._validate_input(inputs)
        gate = self.gate_proj.forward(inputs)
        up = self.up_proj.forward(inputs)
        activated = self.activation.forward(gate, up)
        return self.down_proj.forward(activated)

    def _validate_input(self, inputs: torch.Tensor) -> None:
        if not isinstance(inputs, torch.Tensor):
            raise TypeError("SwiGLUFeedForward input must be a torch.Tensor")
        if inputs.layout is not torch.strided:
            raise TypeError("SwiGLUFeedForward input must use a strided layout")
        if not inputs.is_floating_point():
            raise TypeError("SwiGLUFeedForward input must have a floating-point dtype")
        if not 1 <= inputs.ndim <= MAXIMUM_INPUT_RANK:
            raise ValueError("SwiGLUFeedForward input rank is outside bounds")
        if inputs.numel() == 0:
            raise ValueError("SwiGLUFeedForward input must be nonempty")
        if inputs.shape[-1] != self.hidden_size:
            raise ValueError("SwiGLUFeedForward input feature dimension must equal hidden_size")
        parameter = self.gate_proj.weight
        if inputs.device != parameter.device:
            raise ValueError("SwiGLUFeedForward input and parameters must be on the same device")
        if inputs.dtype != parameter.dtype:
            raise TypeError("SwiGLUFeedForward input and parameters must have the same dtype")

    def extra_repr(self) -> str:
        return (
            f"hidden_size={self.hidden_size}, intermediate_size={self.intermediate_size}, "
            f"bias={self.use_bias}"
        )
