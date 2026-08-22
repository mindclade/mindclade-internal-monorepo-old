# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# mypy: ignore-errors

"""Deterministic expert-major padded grouped GEMM."""

from __future__ import annotations

from typing import Any

from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.providers.tilelang.moe.schedules import GroupedGemmSchedule


def make_grouped_gemm_kernel(
    schedule: GroupedGemmSchedule,
    *,
    target: str | dict[str, str] | None = None,
) -> Any:
    """Create ``Output[e] = Tokens[e] @ Weights[e]`` for padded expert batches.

    Routing, capacity assignment, and unpermutation deliberately stay outside
    this measured primitive.  Every expert owns exactly ``capacity`` rows;
    unused rows must be zeroed by the validated caller.
    """

    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def grouped_gemm(Tokens, Weights):
        experts, capacity, output_dim, reduction_dim = T.const(
            "experts, capacity, output_dim, reduction_dim"
        )
        Tokens: T.Tensor((experts, capacity, reduction_dim), schedule.input_dtype)
        Weights: T.Tensor((experts, reduction_dim, output_dim), schedule.input_dtype)
        Output = T.empty((experts, capacity, output_dim), schedule.output_dtype)

        with T.Kernel(
            T.ceildiv(output_dim, schedule.block_n),
            T.ceildiv(capacity, schedule.block_m),
            experts,
            threads=schedule.threads,
        ) as (column_block, row_block, expert):
            tokens_shared = T.alloc_shared(
                (schedule.block_m, schedule.block_k), schedule.input_dtype
            )
            weights_shared = T.alloc_shared(
                (schedule.block_k, schedule.block_n), schedule.input_dtype
            )
            accumulator = T.alloc_fragment(
                (schedule.block_m, schedule.block_n), schedule.accum_dtype
            )
            T.clear(accumulator)
            for reduction_block in T.Pipelined(
                T.ceildiv(reduction_dim, schedule.block_k),
                num_stages=schedule.num_stages,
            ):
                T.copy(
                    Tokens[
                        expert,
                        row_block * schedule.block_m,
                        reduction_block * schedule.block_k,
                    ],
                    tokens_shared,
                )
                T.copy(
                    Weights[
                        expert,
                        reduction_block * schedule.block_k,
                        column_block * schedule.block_n,
                    ],
                    weights_shared,
                )
                T.gemm(tokens_shared, weights_shared, accumulator)
            T.copy(
                accumulator,
                Output[
                    expert,
                    row_block * schedule.block_m,
                    column_block * schedule.block_n,
                ],
            )
        return Output

    return grouped_gemm
