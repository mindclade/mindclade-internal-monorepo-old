# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Software-pipeline resource and dependency model."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.tilelang.targets.common import TargetSpec


@dataclass(frozen=True, slots=True)
class PipelineSpec:
    stages: int
    producer_bytes_per_stage: int
    persistent_shared_bytes: int = 0
    asynchronous: bool = True

    def __post_init__(self) -> None:
        if self.stages <= 0 or self.stages > 4:
            raise ValueError("pipeline stages must be between one and four")
        if self.producer_bytes_per_stage < 0 or self.persistent_shared_bytes < 0:
            raise ValueError("pipeline memory use must be non-negative")
        if self.stages > 1 and not self.asynchronous:
            raise ValueError("multi-stage pipelines require asynchronous producers")

    @property
    def shared_memory_bytes(self) -> int:
        return self.stages * self.producer_bytes_per_stage + self.persistent_shared_bytes

    def rejection_reason(self, target: TargetSpec) -> str | None:
        if self.shared_memory_bytes > target.capabilities.shared_memory_per_block:
            return "shared_memory_limit"
        if self.asynchronous and not target.capabilities.supports_async_copy:
            return "async_copy_capability"
        return None


def validate_stage_order(stages: tuple[int, ...], order: tuple[int, ...]) -> None:
    if len(stages) != len(order) or not stages:
        raise ValueError("pipeline stage and order annotations must have equal non-zero length")
    if min(stages) < 0 or sorted(order) != list(range(len(order))):
        raise ValueError("pipeline stages must be non-negative and order must be a permutation")
