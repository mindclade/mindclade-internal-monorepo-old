# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Independent equations for fused and Pairformer primitives."""

from __future__ import annotations

from typing import Literal

import torch

from kernels.ops.fused.validation import validate_binary, validate_triangle


def swiglu_reference(gate: torch.Tensor, up: torch.Tensor) -> torch.Tensor:
    validate_binary(gate, up, "SwiGLU")
    result = torch.nn.functional.silu(gate.float()) * up.float()
    return result.to(gate.dtype)


def triangle_multiplication_reference(
    left: torch.Tensor,
    right: torch.Tensor,
    mask: torch.Tensor,
    *,
    orientation: Literal["incoming", "outgoing"],
) -> torch.Tensor:
    """Mask-aware Pairformer triangle multiplication with FP32 reduction."""

    validate_triangle(left, right, mask)
    if orientation == "outgoing":
        result = torch.einsum("bikc,bjkc->bijc", left.float(), right.float())
    elif orientation == "incoming":
        result = torch.einsum("bkic,bkjc->bijc", left.float(), right.float())
    else:
        raise ValueError("orientation must be incoming or outgoing")
    pair_mask = mask[:, :, None] & mask[:, None, :]
    reduction_mask = mask.to(left.dtype)
    if orientation == "outgoing":
        masked_left = left * reduction_mask[:, None, :, None]
        result = torch.einsum("bikc,bjkc->bijc", masked_left.float(), right.float())
    else:
        masked_left = left * reduction_mask[:, :, None, None]
        result = torch.einsum("bkic,bkjc->bijc", masked_left.float(), right.float())
    return (result * pair_mask[..., None]).to(left.dtype)
