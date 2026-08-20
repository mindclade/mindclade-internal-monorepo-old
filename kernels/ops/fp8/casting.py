# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Deterministic saturating FP8 quantization reference."""

from __future__ import annotations

import torch

from kernels.ops.fp8.formats import FP8Format
from kernels.ops.fp8.scaling import QuantizedTensor, per_tensor_scale


def quantize_per_tensor(
    tensor: torch.Tensor, format: FP8Format = FP8Format.E4M3FN
) -> QuantizedTensor:
    if not torch.isfinite(tensor).all():
        raise ValueError("FP8 admission rejects NaN and infinity")
    scale = per_tensor_scale(tensor, format)
    normalized = (tensor.float() / scale).clamp(-format.max_finite, format.max_finite)
    return QuantizedTensor(normalized.to(format.torch_dtype), scale, format)
