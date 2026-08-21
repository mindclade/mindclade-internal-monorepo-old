# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Tensor-core GEMM schedules, including scaled FP8 input paths."""

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass

from kernels.tilelang.targets.common import TargetRequirement, TargetSpec


@dataclass(frozen=True, slots=True)
class GemmSchedule:
    block_m: int
    block_n: int
    block_k: int
    threads: int
    num_stages: int
    input_dtype: str
    output_dtype: str = "bfloat16"
    accum_dtype: str = "float32"

    def __post_init__(self) -> None:
        if self.block_m not in {32, 64, 128, 256} or self.block_n not in {32, 64, 128, 256}:
            raise ValueError("M/N tiles must be powers of two from 32 through 256")
        if self.block_k not in {16, 32, 64, 128}:
            raise ValueError("K tile must be 16, 32, 64, or 128")
        if self.threads not in {128, 256} or self.num_stages not in {1, 2, 3, 4}:
            raise ValueError("unsupported GEMM launch or pipeline configuration")
        if self.input_dtype not in {
            "float16",
            "bfloat16",
            "float8_e4m3fn",
            "float8_e5m2",
        }:
            raise ValueError("unsupported tensor-core input dtype")
        if self.output_dtype not in {"float16", "bfloat16", "float32"}:
            raise ValueError("unsupported GEMM output dtype")
        if self.accum_dtype != "float32":
            raise ValueError("production schedules require FP32 accumulation")

    @property
    def element_bytes(self) -> int:
        return 1 if self.input_dtype.startswith("float8") else 2

    @property
    def shared_memory_bytes(self) -> int:
        per_stage = (self.block_m * self.block_k + self.block_k * self.block_n) * self.element_bytes
        return per_stage * self.num_stages

    def rejection_reason(self, target: TargetSpec, k: int) -> str | None:
        k_alignment = 32 if self.input_dtype.startswith("float8") else 16
        if k % k_alignment:
            return "k_alignment"
        requirement = TargetRequirement(
            dtypes=frozenset({self.input_dtype}),
            min_shared_memory=self.shared_memory_bytes,
            min_threads=self.threads,
            async_copy=self.num_stages > 1,
        )
        return requirement.rejection_reason(target)

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()


def candidate_schedules(input_dtype: str, output_dtype: str) -> tuple[GemmSchedule, ...]:
    block_k = 64 if input_dtype.startswith("float8") else 32
    return tuple(
        GemmSchedule(m, n, block_k, threads, stages, input_dtype, output_dtype)
        for m, n, threads, stages in (
            (64, 64, 128, 1),
            (128, 64, 128, 2),
            (128, 128, 256, 3),
            (64, 256, 256, 3),
        )
    )
