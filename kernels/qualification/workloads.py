# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Deterministic H100 qualification matrix for all production kernel contracts."""

from __future__ import annotations

from dataclasses import dataclass, replace

from kernels.api.specs import ExecutionMode, KernelRequest, TensorLayout, TensorSpec


@dataclass(frozen=True, slots=True)
class WorkloadPair:
    inference: KernelRequest
    training: KernelRequest

    def __post_init__(self) -> None:
        if self.inference.execution_mode != ExecutionMode.INFERENCE:
            raise ValueError("workload inference half has the wrong execution mode")
        if self.training.execution_mode != ExecutionMode.TRAINING:
            raise ValueError("workload training half has the wrong execution mode")


def _pair(request: KernelRequest, gradient_inputs: tuple[int, ...]) -> WorkloadPair:
    return WorkloadPair(
        request,
        replace(
            request,
            execution_mode=ExecutionMode.TRAINING,
            gradient_inputs=gradient_inputs,
        ),
    )


def _attention_pairs() -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(18):
        dtype = ("float16", "bfloat16")[index % 2]
        head_dim = (32, 64, 128)[index % 3]
        batch = 1 + index % 2
        heads = (1, 4, 8)[index % 3]
        query = 17 + index * 7
        key = 19 + index * 5
        q = TensorSpec((batch, heads, query, head_dim), dtype, TensorLayout.BHSD, True, 16)
        kv = TensorSpec((batch, heads, key, head_dim), dtype, TensorLayout.BHSD, True, 16)
        causal = str(bool(index % 2)).lower()
        pairs.append(
            _pair(
                KernelRequest(
                    "attention.sdpa",
                    (q, kv, kv),
                    (q,),
                    "cuda",
                    "sm_90",
                    (("causal", causal), ("mask", "none"), ("scale", "default")),
                ),
                (0, 1, 2),
            )
        )
    return tuple(pairs)


def _gemm_pairs() -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(18):
        dtype = ("float16", "bfloat16", "float8_e4m3fn")[index % 3]
        reduction = (32, 64, 96, 128)[index % 4]
        a = TensorSpec((33 + 7 * index, reduction), dtype, alignment=32)
        b = TensorSpec((reduction, 35 + 5 * index), dtype, alignment=32)
        scale = TensorSpec((1,), "float32", alignment=4)
        output = TensorSpec((a.shape[0], b.shape[1]), "bfloat16", alignment=16)
        activation = ("none", "relu", "silu")[index % 3]
        pairs.append(
            _pair(
                KernelRequest(
                    "fp8.scaled_gemm",
                    (a, b, scale, scale),
                    (output,),
                    "cuda",
                    "sm_90",
                    (("activation", activation), ("scale_granularity", "per_tensor")),
                ),
                (0, 1, 2, 3),
            )
        )
    return tuple(pairs)


def _swiglu_pairs() -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(18):
        value = TensorSpec((17 + index * 3, 31 + index * 11), "bfloat16", alignment=16)
        pairs.append(
            _pair(
                KernelRequest(
                    "fused.swiglu",
                    (value, value),
                    (value,),
                    "cuda",
                    "sm_90",
                ),
                (0, 1),
            )
        )
    return tuple(pairs)


def _diffusion_pairs() -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(18):
        batch = 1 + index % 3
        tokens = 7 + index * 4
        channels = 32 + index * 8
        activation = TensorSpec((batch, tokens, channels), "bfloat16", alignment=16)
        modulation = TensorSpec((batch, channels), "bfloat16", alignment=16)
        pairs.append(
            _pair(
                KernelRequest(
                    "diffusion.modulated_residual",
                    (activation, activation, modulation, modulation, modulation),
                    (activation,),
                    "cuda",
                    "sm_90",
                ),
                (0, 1, 2, 3, 4),
            )
        )
    return tuple(pairs)


def _moe_pairs() -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(18):
        experts = 2 + index % 7
        capacity = 9 + index * 3
        reduction = 32 + (index % 4) * 16
        output_dim = 35 + index * 5
        tokens = TensorSpec(
            (experts, capacity, reduction),
            "bfloat16",
            TensorLayout.EXPERT_MAJOR,
            True,
            16,
        )
        weights = TensorSpec(
            (experts, reduction, output_dim),
            "bfloat16",
            TensorLayout.EXPERT_MAJOR,
            True,
            16,
        )
        output = TensorSpec(
            (experts, capacity, output_dim),
            "bfloat16",
            TensorLayout.EXPERT_MAJOR,
            True,
            16,
        )
        pairs.append(
            _pair(
                KernelRequest(
                    "moe.grouped_gemm",
                    (tokens, weights),
                    (output,),
                    "cuda",
                    "sm_90",
                ),
                (0, 1),
            )
        )
    return tuple(pairs)


def _triangle_pairs(orientation: str) -> tuple[WorkloadPair, ...]:
    pairs = []
    for index in range(17):
        dtype = ("float16", "bfloat16")[index % 2]
        batch = 1 + index % 2
        sequence = 9 + index * 3
        channels = 8 + (index % 5) * 8
        pair = TensorSpec(
            (batch, sequence, sequence, channels),
            dtype,
            TensorLayout.PAIR_MAJOR,
            True,
            16,
        )
        mask = TensorSpec((batch, sequence), "bool")
        pairs.append(
            _pair(
                KernelRequest(
                    f"pairformer.triangle_{orientation}",
                    (pair, pair, mask),
                    (pair,),
                    "cuda",
                    "sm_90",
                ),
                (0, 1),
            )
        )
    return tuple(pairs)


def production_workload_pairs() -> tuple[WorkloadPair, ...]:
    pairs = (
        *_attention_pairs(),
        *_gemm_pairs(),
        *_swiglu_pairs(),
        *_diffusion_pairs(),
        *_moe_pairs(),
        *_triangle_pairs("incoming"),
        *_triangle_pairs("outgoing"),
    )
    if len(pairs) != 124:
        raise AssertionError("the reviewed production workload matrix must contain 124 pairs")
    return pairs


def production_workloads() -> tuple[KernelRequest, ...]:
    return tuple(
        request
        for pair in production_workload_pairs()
        for request in (pair.inference, pair.training)
    )
