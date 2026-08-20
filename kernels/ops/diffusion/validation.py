# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import torch

from kernels.api.errors import KernelValidationError


def validate_modulation(
    normalized: torch.Tensor,
    residual: torch.Tensor,
    scale: torch.Tensor,
    shift: torch.Tensor,
    gate: torch.Tensor,
) -> None:
    if normalized.ndim != 3 or normalized.shape != residual.shape:
        raise KernelValidationError("normalized and residual must share [batch, tokens, channels]")
    expected = (normalized.shape[0], normalized.shape[2])
    if any(item.shape != expected for item in (scale, shift, gate)):
        raise KernelValidationError("scale, shift, and gate must have [batch, channels] shape")
    tensors = (normalized, residual, scale, shift, gate)
    if len({tensor.device for tensor in tensors}) != 1 or len({tensor.dtype for tensor in tensors}) != 1:
        raise KernelValidationError("diffusion modulation tensors must share device and dtype")
    if normalized.numel() == 0 or not normalized.is_floating_point():
        raise KernelValidationError("diffusion modulation requires non-empty floating tensors")


def validate_neighbor_indices(indices: torch.Tensor, *, batch: int, sequence: int) -> None:
    if indices.ndim != 3 or indices.shape[0] != batch or indices.shape[1] != sequence:
        raise KernelValidationError("neighbor indices must have [batch, sequence, neighbors] shape")
    if indices.dtype not in {torch.int32, torch.int64}:
        raise KernelValidationError("neighbor indices must be int32 or int64")
    if indices.numel() == 0:
        raise KernelValidationError("neighbor lists must not be empty")
    if (indices < -1).any() or (indices >= sequence).any():
        raise KernelValidationError("neighbor indices must be -1 or within the sequence")
