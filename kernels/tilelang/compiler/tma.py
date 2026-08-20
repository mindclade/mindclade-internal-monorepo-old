# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""TMA tensor-map and barrier legality; no request-controlled descriptors."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.tilelang.compiler.layouts import StridedLayout
from kernels.tilelang.compiler.swizzle import SharedSwizzle
from kernels.tilelang.targets.common import TargetSpec


@dataclass(frozen=True, slots=True)
class TensorMapSpec:
    global_layout: StridedLayout
    tile_shape: tuple[int, ...]
    swizzle: SharedSwizzle | None = None
    multicast: int = 1

    def __post_init__(self) -> None:
        if not 1 <= len(self.tile_shape) <= 5:
            raise ValueError("TMA tensor maps support ranks one through five")
        if len(self.tile_shape) != len(self.global_layout.shape):
            raise ValueError("TMA tile rank must match global tensor rank")
        if any(dim <= 0 for dim in self.tile_shape) or self.multicast not in {1, 2, 4, 8}:
            raise ValueError("invalid TMA tile or multicast count")
        if self.global_layout.alignment < 16:
            raise ValueError("TMA global tensors require at least 16-byte alignment")
        if any(tile > extent for tile, extent in zip(self.tile_shape, self.global_layout.shape)):
            raise ValueError("TMA tile must fit within its tensor extents")

    def validate_target(self, target: TargetSpec) -> None:
        if target.kind != "cuda" or not target.capabilities.supports_tma:
            raise ValueError("TMA requires an explicitly TMA-capable CUDA target")


@dataclass(frozen=True, slots=True)
class BarrierProtocol:
    participants: int
    transaction_bytes: int

    def __post_init__(self) -> None:
        if self.participants <= 0 or self.transaction_bytes <= 0:
            raise ValueError("barrier participants and transaction bytes must be positive")
