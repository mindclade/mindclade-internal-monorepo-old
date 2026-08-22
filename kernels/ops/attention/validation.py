# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Semantic validation for contiguous BHSD attention."""

from __future__ import annotations

import torch

from kernels.api.errors import KernelValidationError
from kernels.api.validation import (
    require_positive_dimensions,
    require_rank,
    require_same_device,
    require_same_dtype,
)


def validate_qkv(q: torch.Tensor, k: torch.Tensor, v: torch.Tensor) -> None:
    for name, tensor in (("q", q), ("k", k), ("v", v)):
        require_rank(tensor, 4, name)
        require_positive_dimensions(tensor, name)
    require_same_device(("q", q), ("k", k), ("v", v))
    require_same_dtype(("q", q), ("k", k), ("v", v))
    if k.shape != v.shape:
        raise KernelValidationError("k and v must have identical BHSD shapes")
    if q.shape[:2] != k.shape[:2] or q.shape[-1] != k.shape[-1]:
        raise KernelValidationError("q, k, and v must agree on batch, heads, and head dimension")
    if q.dtype not in {torch.float16, torch.bfloat16, torch.float32, torch.float64}:
        raise KernelValidationError("attention requires a floating-point dtype")


def normalize_mask(
    mask: torch.Tensor | None,
    *,
    batch: int,
    heads: int,
    query_length: int,
    key_length: int,
    device: torch.device,
) -> torch.Tensor | None:
    if mask is None:
        return None
    if mask.dtype != torch.bool:
        raise KernelValidationError("attention mask must be boolean (True means allowed)")
    if mask.device != device:
        raise KernelValidationError("attention mask must be on the same device as q")
    try:
        return torch.broadcast_to(mask, (batch, heads, query_length, key_length))
    except RuntimeError as exc:
        raise KernelValidationError(
            "attention mask is not broadcastable to [batch, heads, query, key]"
        ) from exc
