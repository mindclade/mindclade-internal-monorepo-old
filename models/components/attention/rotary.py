# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Interleaved rotary positional embeddings."""

from __future__ import annotations

import math

import torch
from torch import nn


class RotaryEmbedding(nn.Module):
    """Apply interleaved rotary embeddings to ``[..., sequence, head_dim]``.

    Optional ``positions`` is a rank-one integer tensor of length ``sequence``
    on the same device as ``value``. The registered inverse-frequency buffer is
    part of the module state and follows device placement.
    """

    def __init__(self, head_dim: int, *, base: float = 10_000.0) -> None:
        super().__init__()
        if isinstance(head_dim, bool) or not isinstance(head_dim, int):
            raise TypeError("head_dim must be an integer")
        if head_dim <= 0 or head_dim % 2 != 0:
            raise ValueError("head_dim must be a positive even integer")
        if isinstance(base, bool) or not isinstance(base, int | float):
            raise TypeError("rotary base must be a real number")
        resolved_base = float(base)
        if resolved_base <= 1.0 or not math.isfinite(resolved_base):
            raise ValueError("rotary base must be finite and greater than one")
        self.head_dim = head_dim
        self.base = resolved_base
        dimensions = torch.arange(0, head_dim, 2, dtype=torch.float32)
        inverse_frequencies = torch.pow(self.base, -dimensions / head_dim)
        self.inverse_frequencies: torch.Tensor
        self.register_buffer("inverse_frequencies", inverse_frequencies, persistent=True)

    def forward(
        self,
        value: torch.Tensor,
        positions: torch.Tensor | None = None,
    ) -> torch.Tensor:
        if not isinstance(value, torch.Tensor):
            raise TypeError("rotary input must be a tensor")
        if value.ndim < 2 or value.shape[-1] != self.head_dim:
            raise ValueError(f"rotary input must have shape [..., T, {self.head_dim}]")
        if value.shape[-2] == 0:
            raise ValueError("rotary sequence length must be non-zero")
        if not value.is_floating_point():
            raise TypeError("rotary input must use a floating-point dtype")
        if self.inverse_frequencies.device != value.device:
            raise ValueError("rotary module and input must be on the same device")

        sequence_length = value.shape[-2]
        if positions is None:
            positions = torch.arange(sequence_length, device=value.device)
        else:
            if not isinstance(positions, torch.Tensor):
                raise TypeError("rotary positions must be a tensor or None")
            if positions.ndim != 1 or positions.shape[0] != sequence_length:
                raise ValueError("rotary positions must have shape [T]")
            if positions.dtype not in {
                torch.int8,
                torch.int16,
                torch.int32,
                torch.int64,
            }:
                raise TypeError("rotary positions must use an integer dtype")
            if positions.device != value.device:
                raise ValueError("rotary positions must be on the input device")

        compute_dtype = torch.float64 if value.dtype == torch.float64 else torch.float32
        angles = torch.outer(
            positions.to(dtype=compute_dtype),
            self.inverse_frequencies.to(dtype=compute_dtype),
        )
        cosine = angles.cos()
        sine = angles.sin()
        even = value[..., 0::2].to(dtype=compute_dtype)
        odd = value[..., 1::2].to(dtype=compute_dtype)
        rotated = torch.stack(
            (even * cosine - odd * sine, even * sine + odd * cosine),
            dim=-1,
        ).flatten(-2)
        return rotated.to(dtype=value.dtype)
