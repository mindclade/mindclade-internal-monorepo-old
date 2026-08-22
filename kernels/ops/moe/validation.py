# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import torch

from kernels.api.errors import KernelValidationError


def validate_router_logits(logits: torch.Tensor, top_k: int) -> None:
    if logits.ndim != 2 or min(logits.shape) <= 0:
        raise KernelValidationError("router logits must have non-empty [tokens, experts] shape")
    if not logits.is_floating_point() or not torch.isfinite(logits).all():
        raise KernelValidationError("router logits must be finite floating-point values")
    if top_k <= 0 or top_k > logits.shape[1]:
        raise KernelValidationError("top_k must be between one and the expert count")


def validate_grouped_gemm(inputs: torch.Tensor, weights: torch.Tensor) -> None:
    if inputs.ndim != 3 or weights.ndim != 3:
        raise KernelValidationError("grouped GEMM tensors must have rank three")
    if inputs.shape[0] != weights.shape[0] or inputs.shape[2] != weights.shape[1]:
        raise KernelValidationError("expert and reduction dimensions must agree")
    if inputs.device != weights.device or inputs.dtype != weights.dtype:
        raise KernelValidationError("grouped GEMM inputs must share device and dtype")
    if min(inputs.shape) <= 0 or weights.shape[2] <= 0:
        raise KernelValidationError("grouped GEMM dimensions must be positive")
