# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Producer/consumer warp-group ownership model."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.tilelang.targets.common import TargetSpec


@dataclass(frozen=True, slots=True)
class WarpSpecialization:
    producer_warps: int
    consumer_warps: int
    register_cap_producer: int
    register_cap_consumer: int

    def __post_init__(self) -> None:
        if self.producer_warps <= 0 or self.consumer_warps <= 0:
            raise ValueError("producer and consumer warp counts must be positive")
        if min(self.register_cap_producer, self.register_cap_consumer) <= 0:
            raise ValueError("register caps must be positive")

    def validate_target(self, target: TargetSpec) -> None:
        threads = (self.producer_warps + self.consumer_warps) * target.capabilities.warp_size
        if target.kind != "cuda" or not target.capabilities.supports_wgmma:
            raise ValueError("warp specialization requires a reviewed WGMMA-capable CUDA target")
        if threads > target.capabilities.max_threads_per_block:
            raise ValueError("warp specialization exceeds the target thread limit")
