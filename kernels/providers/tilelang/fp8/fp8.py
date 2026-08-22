# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# mypy: ignore-errors

"""Pipelined TileLang GEMM with explicit per-tensor dequantization scales."""

from __future__ import annotations

from typing import Any

from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.providers.tilelang.fp8.schedules import GemmSchedule


def make_scaled_gemm_kernel(
    schedule: GemmSchedule,
    *,
    target: str | dict[str, str] | None = None,
    activation: str = "none",
) -> Any:
    """Create ``(A * a_scale) @ (B * b_scale)`` with an optional fused activation.

    Scales are one-element FP32 tensors, keeping their values runtime-visible and
    preventing scale-specialized cache explosion.  FP32 accumulation occurs before
    scaling and output conversion.
    """

    if activation not in {"none", "relu", "silu"}:
        raise ValueError("activation must be none, relu, or silu")
    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def scaled_gemm(A, B, AScale, BScale):
        m, n, k = T.const("m, n, k")
        A: T.Tensor((m, k), schedule.input_dtype)
        B: T.Tensor((k, n), schedule.input_dtype)
        AScale: T.Tensor((1,), "float32")
        BScale: T.Tensor((1,), "float32")
        Output = T.empty((m, n), schedule.output_dtype)

        with T.Kernel(
            T.ceildiv(n, schedule.block_n),
            T.ceildiv(m, schedule.block_m),
            threads=schedule.threads,
        ) as (block_n, block_m):
            a_shared = T.alloc_shared((schedule.block_m, schedule.block_k), schedule.input_dtype)
            b_shared = T.alloc_shared((schedule.block_k, schedule.block_n), schedule.input_dtype)
            accumulator = T.alloc_fragment(
                (schedule.block_m, schedule.block_n), schedule.accum_dtype
            )
            T.clear(accumulator)

            for k_block in T.Pipelined(
                T.ceildiv(k, schedule.block_k), num_stages=schedule.num_stages
            ):
                for row, reduction in T.Parallel(schedule.block_m, schedule.block_k):
                    global_row = block_m * schedule.block_m + row
                    global_k = k_block * schedule.block_k + reduction
                    value = 0.0
                    if global_row < m and global_k < k:
                        value = A[global_row, global_k]
                    a_shared[row, reduction] = value
                for reduction, column in T.Parallel(schedule.block_k, schedule.block_n):
                    global_k = k_block * schedule.block_k + reduction
                    global_column = block_n * schedule.block_n + column
                    value = 0.0
                    if global_k < k and global_column < n:
                        value = B[global_k, global_column]
                    b_shared[reduction, column] = value
                T.gemm(a_shared, b_shared, accumulator)

            for row, column in T.Parallel(schedule.block_m, schedule.block_n):
                value = accumulator[row, column] * AScale[0] * BScale[0]
                if activation == "relu":
                    value = T.max(value, 0.0)
                elif activation == "silu":
                    value = value * T.sigmoid(value)
                global_row = block_m * schedule.block_m + row
                global_column = block_n * schedule.block_n + column
                if global_row < m and global_column < n:
                    Output[global_row, global_column] = value
        return Output

    return scaled_gemm
