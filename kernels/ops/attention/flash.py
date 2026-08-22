# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""TileLang FlashAttention source adapter with strict domain checks."""

from __future__ import annotations

from typing import Any

import torch

from kernels.api.validation import require_contiguous
from kernels.ops.attention.validation import validate_qkv
from kernels.providers.tilelang.attention import (
    FlashAttentionSchedule,
    make_flash_attention_kernel,
)


def flash_attention(
    q: torch.Tensor,
    k: torch.Tensor,
    v: torch.Tensor,
    *,
    causal: bool,
    schedule: FlashAttentionSchedule,
    target: str | dict[str, str] | None = None,
) -> Any:
    validate_qkv(q, k, v)
    require_contiguous(("q", q), ("k", k), ("v", v))
    if q.dtype not in {torch.float16, torch.bfloat16}:
        raise ValueError("TileLang FlashAttention supports fp16 and bf16 inputs")
    if q.shape[-1] not in {32, 64, 128, 256}:
        raise ValueError("TileLang FlashAttention requires head dimension 32, 64, 128, or 256")
    kernel = make_flash_attention_kernel(schedule, target=target)
    return kernel(q, k, v, causal=causal)
