# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

# mypy: ignore-errors

"""Fused adaptive modulation, gating, and residual addition for diffusion blocks."""

from __future__ import annotations

from typing import Any

from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.providers.tilelang.diffusion.schedules import DiffusionEpilogueSchedule


def make_modulated_residual_kernel(
    schedule: DiffusionEpilogueSchedule,
    *,
    dtype: str = "bfloat16",
    target: str | dict[str, str] | None = None,
) -> Any:
    """Create ``residual + gate * (normalized * (1 + scale) + shift)``.

    ``scale``, ``shift``, and ``gate`` use ``[batch, channels]`` layout and
    broadcast over tokens without a materialized expansion.
    """

    if dtype not in {"float16", "bfloat16", "float32"}:
        raise ValueError("diffusion epilogue dtype must be fp16, bf16, or fp32")
    tilelang, T = _tilelang()
    jit = tilelang.jit if target is None else tilelang.jit(target=target)

    @jit
    def modulated_residual(Normalized, Residual, Scale, Shift, Gate):
        batch, tokens, channels = T.const("batch, tokens, channels")
        Normalized: T.Tensor((batch, tokens, channels), dtype)
        Residual: T.Tensor((batch, tokens, channels), dtype)
        Scale: T.Tensor((batch, channels), dtype)
        Shift: T.Tensor((batch, channels), dtype)
        Gate: T.Tensor((batch, channels), dtype)
        Output = T.empty((batch, tokens, channels), dtype)
        tile_size = schedule.threads * schedule.vector_width

        with T.Kernel(
            T.ceildiv(batch * tokens * channels, tile_size), threads=schedule.threads
        ) as block:
            for thread, lane in T.Parallel(schedule.threads, schedule.vector_width):
                index = block * tile_size + thread * schedule.vector_width + lane
                if index < batch * tokens * channels:
                    channel = index % channels
                    token = (index // channels) % tokens
                    batch_index = index // (tokens * channels)
                    value = T.cast(Normalized[batch_index, token, channel], "float32")
                    value = value * (1.0 + Scale[batch_index, channel])
                    value += Shift[batch_index, channel]
                    value *= Gate[batch_index, channel]
                    value += Residual[batch_index, token, channel]
                    Output[batch_index, token, channel] = value
        return Output

    return modulated_residual
