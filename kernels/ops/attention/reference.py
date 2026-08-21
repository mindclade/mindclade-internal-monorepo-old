# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Independent, stable attention equation used for numerical qualification."""

from __future__ import annotations

import math

import torch

from kernels.ops.attention.validation import normalize_mask, validate_qkv


def attention_reference(
    q: torch.Tensor,
    k: torch.Tensor,
    v: torch.Tensor,
    *,
    mask: torch.Tensor | None = None,
    causal: bool = False,
    scale: float | None = None,
) -> torch.Tensor:
    """Compute BHSD attention with FP32-or-better reductions and zero all-masked rows.

    A boolean mask uses ``True`` for allowed keys.  The causal mask is composed
    with it.  Unlike a raw softmax over all ``-inf``, an all-masked row is
    explicitly defined to produce zeros rather than NaNs.
    """

    validate_qkv(q, k, v)
    batch, heads, query_length, head_dim = q.shape
    key_length = k.shape[-2]
    allowed = normalize_mask(
        mask,
        batch=batch,
        heads=heads,
        query_length=query_length,
        key_length=key_length,
        device=q.device,
    )
    if causal:
        query_positions = torch.arange(query_length, device=q.device)[:, None]
        key_positions = torch.arange(key_length, device=q.device)[None, :]
        causal_mask = key_positions <= query_positions
        allowed = causal_mask if allowed is None else allowed & causal_mask

    effective_scale = math.sqrt(1.0 / head_dim) if scale is None else float(scale)
    reduction_dtype = torch.float64 if q.dtype == torch.float64 else torch.float32
    scores = (
        torch.matmul(
            q.to(reduction_dtype),
            k.to(reduction_dtype).transpose(-1, -2),
        )
        * effective_scale
    )
    if allowed is None:
        probabilities = torch.softmax(scores, dim=-1)
    else:
        row_has_key = allowed.any(dim=-1, keepdim=True)
        masked_scores = scores.masked_fill(~allowed, -torch.inf)
        safe_scores = torch.where(row_has_key, masked_scores, torch.zeros_like(masked_scores))
        probabilities = torch.softmax(safe_scores, dim=-1)
        probabilities = torch.where(row_has_key, probabilities, torch.zeros_like(probabilities))
    return torch.matmul(probabilities, v.to(reduction_dtype)).to(q.dtype)
