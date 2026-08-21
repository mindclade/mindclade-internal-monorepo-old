# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Stable attention API; PyTorch remains the default semantic authority."""

from __future__ import annotations

import torch

from kernels.ops.attention.reference import attention_reference


def scaled_dot_product_attention(
    q: torch.Tensor,
    k: torch.Tensor,
    v: torch.Tensor,
    *,
    mask: torch.Tensor | None = None,
    causal: bool = False,
    scale: float | None = None,
) -> torch.Tensor:
    """Provider-neutral attention.

    Qualification-gated dispatch can replace this reference at a model-owned
    adapter.  This function intentionally never enables unqualified JIT source.
    """

    return attention_reference(q, k, v, mask=mask, causal=causal, scale=scale)
