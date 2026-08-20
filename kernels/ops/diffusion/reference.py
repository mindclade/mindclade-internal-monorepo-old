# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Diffusion operator references with exact broadcast and sparse-index semantics."""

from __future__ import annotations

import math

import torch

from kernels.ops.attention.validation import validate_qkv
from kernels.ops.diffusion.validation import validate_modulation, validate_neighbor_indices


def modulated_residual_reference(
    normalized: torch.Tensor,
    residual: torch.Tensor,
    scale: torch.Tensor,
    shift: torch.Tensor,
    gate: torch.Tensor,
) -> torch.Tensor:
    validate_modulation(normalized, residual, scale, shift, gate)
    result = normalized.float() * (1.0 + scale[:, None, :].float())
    result = result + shift[:, None, :].float()
    result = residual.float() + gate[:, None, :].float() * result
    return result.to(normalized.dtype)


def neighbor_attention_reference(
    q: torch.Tensor,
    k: torch.Tensor,
    v: torch.Tensor,
    neighbor_indices: torch.Tensor,
    *,
    scale: float | None = None,
) -> torch.Tensor:
    """Gather-only sparse attention; ``-1`` is padding and all-padding rows return zero."""

    validate_qkv(q, k, v)
    batch, heads, sequence, head_dim = q.shape
    if k.shape[-2] != sequence:
        raise ValueError("neighbor attention requires equal query and key sequence lengths")
    validate_neighbor_indices(neighbor_indices, batch=batch, sequence=sequence)
    if neighbor_indices.device != q.device:
        raise ValueError("neighbor indices must be on the same device as q")

    valid = neighbor_indices >= 0
    safe = neighbor_indices.clamp_min(0).long()
    batch_index = torch.arange(batch, device=q.device)[:, None, None]
    k_bshd = k.permute(0, 2, 1, 3)
    v_bshd = v.permute(0, 2, 1, 3)
    gathered_k = k_bshd[batch_index, safe].permute(0, 3, 1, 2, 4)
    gathered_v = v_bshd[batch_index, safe].permute(0, 3, 1, 2, 4)

    effective_scale = math.sqrt(1.0 / head_dim) if scale is None else float(scale)
    scores = torch.einsum("bhnd,bhnkd->bhnk", q.float(), gathered_k.float())
    scores = scores * effective_scale
    expanded_valid = valid[:, None, :, :]
    row_has_key = expanded_valid.any(dim=-1, keepdim=True)
    scores = scores.masked_fill(~expanded_valid, -torch.inf)
    scores = torch.where(row_has_key, scores, torch.zeros_like(scores))
    probabilities = torch.softmax(scores, dim=-1)
    probabilities = torch.where(row_has_key, probabilities, torch.zeros_like(probabilities))
    return torch.einsum("bhnk,bhnkd->bhnd", probabilities, gathered_v.float()).to(q.dtype)
