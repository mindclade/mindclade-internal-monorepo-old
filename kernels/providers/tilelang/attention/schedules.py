# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded FlashAttention schedules and resource legality."""

from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass

from kernels.tilelang.targets.common import TargetRequirement, TargetSpec


@dataclass(frozen=True, slots=True)
class FlashAttentionSchedule:
    block_m: int
    block_n: int
    threads: int
    num_stages: int
    dtype: str = "float16"
    accum_dtype: str = "float32"

    def __post_init__(self) -> None:
        if self.block_m not in {32, 64, 128} or self.block_n not in {32, 64, 128}:
            raise ValueError("attention tiles must be one of 32, 64, or 128")
        if self.threads not in {128, 256}:
            raise ValueError("attention schedules use 128 or 256 threads")
        if self.num_stages not in {1, 2, 3, 4}:
            raise ValueError("pipeline stage count must be between one and four")
        if self.dtype not in {"float16", "bfloat16"} or self.accum_dtype != "float32":
            raise ValueError("attention supports fp16/bf16 inputs with fp32 accumulation")

    def shared_memory_bytes(self, head_dim: int) -> int:
        element_bytes = 2
        q_bytes = self.block_m * head_dim * element_bytes
        kv_bytes = 2 * self.block_n * head_dim * element_bytes * self.num_stages
        probability_bytes = self.block_m * self.block_n * element_bytes
        return q_bytes + kv_bytes + probability_bytes

    def rejection_reason(self, target: TargetSpec, head_dim: int) -> str | None:
        if head_dim not in {32, 64, 128, 256} or head_dim % 16:
            return "head_dimension"
        requirement = TargetRequirement(
            dtypes=frozenset({self.dtype}),
            min_shared_memory=self.shared_memory_bytes(head_dim),
            min_threads=self.threads,
            async_copy=self.num_stages > 1,
        )
        return requirement.rejection_reason(target)

    @property
    def digest(self) -> str:
        payload = json.dumps(asdict(self), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode()).hexdigest()


CONSERVATIVE_FLASH = FlashAttentionSchedule(32, 32, 128, 1)
HOPPER_FLASH = FlashAttentionSchedule(64, 64, 256, 3)
BLACKWELL_FLASH = FlashAttentionSchedule(128, 64, 256, 3)


def candidate_schedules(dtype: str) -> tuple[FlashAttentionSchedule, ...]:
    """Small search space; legality is checked against the exact target later."""

    return tuple(
        FlashAttentionSchedule(block_m, block_n, threads, stages, dtype=dtype)
        for block_m, block_n, threads, stages in (
            (32, 32, 128, 1),
            (64, 64, 128, 2),
            (64, 64, 256, 3),
            (128, 64, 256, 3),
        )
    )
