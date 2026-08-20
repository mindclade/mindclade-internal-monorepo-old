# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Pure eligibility predicates for the TileLang source candidates."""

from __future__ import annotations

from collections.abc import Callable

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import KernelRequest


def exact_eligibility(
    *,
    targets: frozenset[str],
    architectures: frozenset[str],
    dtypes: frozenset[str],
    shape_check: Callable[[KernelRequest], str | None] | None = None,
) -> Callable[[KernelRequest, DeviceCapabilities], str | None]:
    def check(request: KernelRequest, capabilities: DeviceCapabilities) -> str | None:
        if request.target not in targets or capabilities.target != request.target:
            return "target"
        if (
            request.architecture not in architectures
            or capabilities.architecture != request.architecture
        ):
            return "architecture"
        input_dtypes = {spec.dtype for spec in request.inputs}
        if not input_dtypes.issubset(dtypes):
            return "dtype"
        if any(not spec.contiguous for spec in request.inputs):
            return "layout_contiguity"
        return None if shape_check is None else shape_check(request)

    return check


def attention_shape(request: KernelRequest) -> str | None:
    if len(request.inputs) != 3 or any(len(spec.shape) != 4 for spec in request.inputs):
        return "attention_rank"
    q, k, v = request.inputs
    if k.shape != v.shape or q.shape[:2] != k.shape[:2] or q.shape[-1] != k.shape[-1]:
        return "attention_shape"
    if q.shape[-1] not in {32, 64, 128, 256}:
        return "head_dimension"
    return None


def gemm_shape(request: KernelRequest) -> str | None:
    if len(request.inputs) < 2 or any(len(spec.shape) != 2 for spec in request.inputs[:2]):
        return "gemm_rank"
    a, b = request.inputs[:2]
    if a.shape[1] != b.shape[0]:
        return "reduction_dimension"
    alignment = 32 if a.dtype.startswith("float8") else 16
    return None if a.shape[1] % alignment == 0 else "reduction_alignment"
