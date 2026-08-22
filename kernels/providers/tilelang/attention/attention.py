# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# mypy: ignore-errors

"""TileLang online-softmax attention kernel.

The kernel consumes contiguous BHSD tensors, performs FP32 online softmax, and
never materializes the quadratic attention matrix in global memory.  It is a
source candidate only; runtime dispatch additionally requires immutable
qualification for the exact signature and target.
"""

from __future__ import annotations

from typing import Any

from kernels.api.errors import KernelErrorCode, KernelUnavailableError
from kernels.providers.tilelang.attention.schedules import FlashAttentionSchedule


def _tilelang() -> tuple[Any, Any]:
    try:
        import tilelang
        import tilelang.language as T
    except ImportError as exc:
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "TileLang 0.1.13 is not installed in this accelerator environment",
        ) from exc
    version = getattr(tilelang, "__version__", "")
    if version != "0.1.13":
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "the TileLang provider requires exactly version 0.1.13",
            details={"observed_version": version},
        )
    return tilelang, T


def make_flash_attention_kernel(
    schedule: FlashAttentionSchedule,
    *,
    target: str | dict[str, str] | None = None,
) -> Any:
    """Build an eager TileLang kernel for dense or causal self/cross attention."""

    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def flash_attention(Q, K, V, causal: bool = False):
        batch, heads, query_length, key_length, head_dim = T.const(
            "batch, heads, query_length, key_length, head_dim"
        )
        Q: T.Tensor((batch, heads, query_length, head_dim), schedule.dtype)
        K: T.Tensor((batch, heads, key_length, head_dim), schedule.dtype)
        V: T.Tensor((batch, heads, key_length, head_dim), schedule.dtype)
        Output = T.empty((batch, heads, query_length, head_dim), schedule.dtype)

        with T.Kernel(
            T.ceildiv(query_length, schedule.block_m), batch * heads, threads=schedule.threads
        ) as (query_block, batch_head):
            batch_index = batch_head // heads
            head_index = batch_head % heads
            query_offset = query_block * schedule.block_m

            q_shared = T.alloc_shared((schedule.block_m, head_dim), schedule.dtype)
            k_shared = T.alloc_shared((schedule.block_n, head_dim), schedule.dtype)
            v_shared = T.alloc_shared((schedule.block_n, head_dim), schedule.dtype)
            probabilities_shared = T.alloc_shared(
                (schedule.block_m, schedule.block_n), schedule.dtype
            )
            scores = T.alloc_fragment((schedule.block_m, schedule.block_n), schedule.accum_dtype)
            output = T.alloc_fragment((schedule.block_m, head_dim), schedule.accum_dtype)
            row_max = T.alloc_fragment((schedule.block_m,), schedule.accum_dtype)
            row_max_previous = T.alloc_fragment((schedule.block_m,), schedule.accum_dtype)
            row_sum = T.alloc_fragment((schedule.block_m,), schedule.accum_dtype)
            tile_sum = T.alloc_fragment((schedule.block_m,), schedule.accum_dtype)

            T.copy(Q[batch_index, head_index, query_offset, 0], q_shared)
            T.clear(output)
            T.fill(row_max, -T.infinity(schedule.accum_dtype))
            T.clear(row_sum)
            softmax_scale = T.rsqrt(T.cast(head_dim, schedule.accum_dtype))

            for key_block in T.Pipelined(
                T.ceildiv(key_length, schedule.block_n), num_stages=schedule.num_stages
            ):
                key_offset = key_block * schedule.block_n
                T.copy(K[batch_index, head_index, key_offset, 0], k_shared)
                T.copy(V[batch_index, head_index, key_offset, 0], v_shared)
                T.clear(scores)
                T.gemm(q_shared, k_shared, scores, transpose_B=True)

                for row, column in T.Parallel(schedule.block_m, schedule.block_n):
                    query_index = query_offset + row
                    key_index = key_offset + column
                    allowed = (key_index < key_length) and (
                        (not causal) or (key_index <= query_index)
                    )
                    scores[row, column] = T.if_then_else(
                        allowed,
                        scores[row, column] * softmax_scale,
                        -T.infinity(schedule.accum_dtype),
                    )

                T.copy(row_max, row_max_previous)
                T.reduce_max(scores, row_max, dim=1, clear=True)
                for row in T.Parallel(schedule.block_m):
                    row_max[row] = T.max(row_max[row], row_max_previous[row])

                for row, column in T.Parallel(schedule.block_m, schedule.block_n):
                    scores[row, column] = T.exp2(
                        (scores[row, column] - row_max[row]) * 1.4426950408889634
                    )

                T.reduce_sum(scores, tile_sum, dim=1, clear=True)
                for row, column in T.Parallel(schedule.block_m, head_dim):
                    output[row, column] *= T.exp2(
                        (row_max_previous[row] - row_max[row]) * 1.4426950408889634
                    )
                for row in T.Parallel(schedule.block_m):
                    row_sum[row] = (
                        row_sum[row]
                        * T.exp2((row_max_previous[row] - row_max[row]) * 1.4426950408889634)
                        + tile_sum[row]
                    )

                T.copy(scores, probabilities_shared)
                T.gemm(probabilities_shared, v_shared, output)

            for row, column in T.Parallel(schedule.block_m, head_dim):
                output[row, column] /= row_sum[row]
            T.copy(output, Output[batch_index, head_index, query_offset, 0])

        return Output

    return flash_attention
