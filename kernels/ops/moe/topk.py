# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from dataclasses import dataclass

import torch

from kernels.ops.moe.validation import validate_router_logits


@dataclass(frozen=True, slots=True)
class RoutingDecision:
    expert_indices: torch.Tensor
    weights: torch.Tensor


def stable_topk(logits: torch.Tensor, top_k: int) -> RoutingDecision:
    """Stable expert selection: lower expert index wins an exact tie."""

    validate_router_logits(logits, top_k)
    ordering = torch.argsort(logits.float(), dim=-1, descending=True, stable=True)
    indices = ordering[:, :top_k]
    selected = torch.gather(logits.float(), 1, indices)
    weights = torch.softmax(selected, dim=-1)
    return RoutingDecision(indices, weights)
