# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from typing import Any

import torch

from kernels.api.validation import require_contiguous
from kernels.ops.fp8.validation import validate_scaled_gemm
from kernels.providers.tilelang.fp8 import GemmSchedule, make_scaled_gemm_kernel


def tilelang_scaled_gemm(
    a: torch.Tensor,
    b: torch.Tensor,
    a_scale: torch.Tensor,
    b_scale: torch.Tensor,
    *,
    schedule: GemmSchedule,
    activation: str = "none",
    target: str | dict[str, str] | None = None,
) -> Any:
    validate_scaled_gemm(a, b, a_scale, b_scale)
    require_contiguous(("a", a), ("b", b), ("a_scale", a_scale), ("b_scale", b_scale))
    expected = getattr(torch, schedule.input_dtype)
    if a.dtype != expected or b.dtype != expected:
        raise ValueError(f"inputs must use schedule dtype {schedule.input_dtype}")
    kernel = make_scaled_gemm_kernel(schedule, target=target, activation=activation)
    return kernel(a, b, a_scale, b_scale)
