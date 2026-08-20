# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""WGMMA legality contract for Hopper/Blackwell schedules."""

from __future__ import annotations

from dataclasses import dataclass

from kernels.tilelang.targets.common import TargetSpec


@dataclass(frozen=True, slots=True)
class WGMMAInstruction:
    m: int
    n: int
    k: int
    input_dtype: str
    accum_dtype: str = "float32"

    def __post_init__(self) -> None:
        if self.m != 64 or self.n not in {8, 16, 32, 64, 128, 256}:
            raise ValueError("unsupported WGMMA M/N instruction shape")
        expected_k = 32 if self.input_dtype.startswith("float8") else 16
        if self.k != expected_k or self.accum_dtype != "float32":
            raise ValueError("unsupported WGMMA K dimension or accumulator dtype")

    def validate_target(self, target: TargetSpec) -> None:
        if not target.capabilities.supports_wgmma:
            raise ValueError("WGMMA requires an explicitly qualified architecture capability")
        if not target.capabilities.supports_dtype(self.input_dtype):
            raise ValueError("target does not support the WGMMA input dtype")
