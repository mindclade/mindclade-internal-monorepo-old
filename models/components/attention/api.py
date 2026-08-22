# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral scaled-dot-product attention operator contract."""

from __future__ import annotations

import torch
from torch import nn
from torch.nn import functional as F


class AttentionOperator(nn.Module):
    """Injected attention primitive operating on projected ``[B, H, T, D]`` tensors.

    Boolean masks use ``True`` for an allowed query/key pair. Implementations must
    return a tensor with the same shape, dtype, and device as ``query``.
    """

    def forward(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
        *,
        mask: torch.Tensor | None,
        causal: bool,
        scale: float | None,
        dropout_p: float,
    ) -> torch.Tensor:
        raise NotImplementedError


class PyTorchSDPAOperator(AttentionOperator):
    """PyTorch SDPA semantic reference and safe default implementation."""

    def forward(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
        *,
        mask: torch.Tensor | None,
        causal: bool,
        scale: float | None,
        dropout_p: float,
    ) -> torch.Tensor:
        return F.scaled_dot_product_attention(
            query,
            key,
            value,
            attn_mask=mask,
            dropout_p=dropout_p,
            is_causal=causal,
            scale=scale,
        )
