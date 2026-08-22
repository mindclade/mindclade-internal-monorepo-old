# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# mypy: ignore-errors

"""TileLang fused SwiGLU and Pairformer triangle multiplication kernels."""

from __future__ import annotations

from typing import Any, Literal

from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.providers.tilelang.fused.schedules import ElementwiseSchedule, TriangleSchedule


def make_swiglu_kernel(
    schedule: ElementwiseSchedule,
    *,
    dtype: str = "bfloat16",
    target: str | dict[str, str] | None = None,
) -> Any:
    if dtype not in {"float16", "bfloat16", "float32"}:
        raise ValueError("SwiGLU dtype must be fp16, bf16, or fp32")
    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def swiglu(Gate, Up):
        rows, columns = T.const("rows, columns")
        Gate: T.Tensor((rows, columns), dtype)
        Up: T.Tensor((rows, columns), dtype)
        Output = T.empty((rows, columns), dtype)
        tile_size = schedule.threads * schedule.vector_width

        with T.Kernel(T.ceildiv(rows * columns, tile_size), threads=schedule.threads) as block:
            for thread, lane in T.Parallel(schedule.threads, schedule.vector_width):
                index = block * tile_size + thread * schedule.vector_width + lane
                if index < rows * columns:
                    row = index // columns
                    column = index % columns
                    value = T.cast(Gate[row, column], "float32")
                    Output[row, column] = value * T.sigmoid(value) * Up[row, column]
        return Output

    return swiglu


def make_triangle_multiplication_kernel(
    schedule: TriangleSchedule,
    *,
    orientation: Literal["incoming", "outgoing"],
    target: str | dict[str, str] | None = None,
) -> Any:
    """Create a mask-aware ``[B, N, N, C]`` Pairformer contraction.

    Outgoing computes ``sum_k left[b,i,k,c] * right[b,j,k,c]``;
    incoming computes ``sum_k left[b,k,i,c] * right[b,k,j,c]``.
    """

    if orientation not in {"incoming", "outgoing"}:
        raise ValueError("orientation must be incoming or outgoing")
    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def triangle_multiplication(Left, Right, Mask):
        batch, sequence, channels = T.const("batch, sequence, channels")
        Left: T.Tensor((batch, sequence, sequence, channels), schedule.dtype)
        Right: T.Tensor((batch, sequence, sequence, channels), schedule.dtype)
        Mask: T.Tensor((batch, sequence), "uint8")
        Output = T.empty((batch, sequence, sequence, channels), schedule.dtype)

        with T.Kernel(
            T.ceildiv(sequence, schedule.block_n),
            T.ceildiv(sequence, schedule.block_m),
            batch * channels,
            threads=schedule.threads,
        ) as (column_block, row_block, batch_channel):
            batch_index = batch_channel // channels
            channel = batch_channel % channels
            row_offset = row_block * schedule.block_m
            column_offset = column_block * schedule.block_n
            left_shared = T.alloc_shared((schedule.block_m, schedule.block_k), schedule.dtype)
            right_shared = T.alloc_shared((schedule.block_k, schedule.block_n), schedule.dtype)
            accumulator = T.alloc_fragment(
                (schedule.block_m, schedule.block_n), schedule.accum_dtype
            )
            T.clear(accumulator)

            for reduction_block in T.Pipelined(
                T.ceildiv(sequence, schedule.block_k), num_stages=schedule.num_stages
            ):
                reduction_offset = reduction_block * schedule.block_k
                for row, reduction in T.Parallel(schedule.block_m, schedule.block_k):
                    i = row_offset + row
                    k = reduction_offset + reduction
                    value = 0.0
                    if i < sequence and k < sequence and Mask[batch_index, k] != 0:
                        if orientation == "outgoing":
                            value = Left[batch_index, i, k, channel]
                        else:
                            value = Left[batch_index, k, i, channel]
                    left_shared[row, reduction] = value

                for reduction, column in T.Parallel(schedule.block_k, schedule.block_n):
                    k = reduction_offset + reduction
                    j = column_offset + column
                    value = 0.0
                    if j < sequence and k < sequence and Mask[batch_index, k] != 0:
                        if orientation == "outgoing":
                            value = Right[batch_index, j, k, channel]
                        else:
                            value = Right[batch_index, k, j, channel]
                    right_shared[reduction, column] = value

                T.gemm(left_shared, right_shared, accumulator)

            for row, column in T.Parallel(schedule.block_m, schedule.block_n):
                i = row_offset + row
                j = column_offset + column
                if i < sequence and j < sequence:
                    valid = Mask[batch_index, i] != 0 and Mask[batch_index, j] != 0
                    Output[batch_index, i, j, channel] = T.if_then_else(
                        valid, accumulator[row, column], 0.0
                    )
        return Output

    return triangle_multiplication
