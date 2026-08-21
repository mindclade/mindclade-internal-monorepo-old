# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import functools
from collections.abc import Callable

from kernels.ops.attention.reference import attention_reference
from kernels.ops.diffusion.reference import modulated_residual_reference
from kernels.ops.fp8.reference import scaled_gemm_reference
from kernels.ops.fused.reference import swiglu_reference, triangle_multiplication_reference
from kernels.ops.moe.grouped_gemm import padded_grouped_gemm_reference
from kernels.providers.pytorch.adapter import reference_registration
from kernels.registry import KernelRegistry


def register_references(registry: KernelRegistry) -> None:
    implementations: dict[str, Callable[..., object]] = {
        "attention.sdpa": attention_reference,
        "diffusion.modulated_residual": modulated_residual_reference,
        "fp8.scaled_gemm": scaled_gemm_reference,
        "fused.swiglu": swiglu_reference,
        "moe.grouped_gemm": padded_grouped_gemm_reference,
        "pairformer.triangle_incoming": functools.partial(
            triangle_multiplication_reference, orientation="incoming"
        ),
        "pairformer.triangle_outgoing": functools.partial(
            triangle_multiplication_reference, orientation="outgoing"
        ),
    }
    for operation, invoke in implementations.items():
        registry.register(reference_registration(operation, invoke))
