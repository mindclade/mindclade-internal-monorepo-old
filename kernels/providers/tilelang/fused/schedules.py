# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Schedules for Pairformer contractions and bandwidth-bound fused kernels."""

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass

from kernels.tilelang.targets.common import TargetRequirement, TargetSpec


@dataclass(frozen=True, slots=True)
class TriangleSchedule:
    block_m: int = 64
    block_n: int = 64
    block_k: int = 32
    threads: int = 128
    num_stages: int = 2
    dtype: str = "bfloat16"
    accum_dtype: str = "float32"

    def __post_init__(self) -> None:
        if (self.block_m, self.block_n) not in {(32, 32), (64, 64), (128, 64)}:
            raise ValueError("unsupported triangle tile")
        if self.block_k not in {16, 32, 64}:
            raise ValueError("unsupported triangle reduction tile")
        if self.threads not in {128, 256} or self.num_stages not in {1, 2, 3}:
            raise ValueError("unsupported triangle launch configuration")
        if self.dtype not in {"float16", "bfloat16"} or self.accum_dtype != "float32":
            raise ValueError("triangle kernels require fp16/bf16 input and fp32 accumulation")

    @property
    def shared_memory_bytes(self) -> int:
        return (self.block_m * self.block_k + self.block_k * self.block_n) * 2 * self.num_stages

    def rejection_reason(self, target: TargetSpec, sequence_length: int) -> str | None:
        if sequence_length <= 0:
            return "sequence_length"
        return TargetRequirement(
            dtypes=frozenset({self.dtype}),
            min_shared_memory=self.shared_memory_bytes,
            min_threads=self.threads,
            async_copy=self.num_stages > 1,
        ).rejection_reason(target)

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()


@dataclass(frozen=True, slots=True)
class ElementwiseSchedule:
    threads: int = 256
    vector_width: int = 4

    def __post_init__(self) -> None:
        if self.threads not in {128, 256, 512} or self.vector_width not in {1, 2, 4, 8}:
            raise ValueError("unsupported elementwise launch configuration")
