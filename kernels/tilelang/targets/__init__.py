# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Qualified-target candidates and exact capability lookup."""

from kernels.tilelang.targets.amd_cdna import CDNA2_GFX90A, CDNA3_GFX942, CDNA4_GFX950
from kernels.tilelang.targets.blackwell import BLACKWELL_SM100, BLACKWELL_SM120
from kernels.tilelang.targets.common import TargetRequirement, TargetSpec
from kernels.tilelang.targets.hopper import HOPPER

TARGETS: dict[tuple[str, str], TargetSpec] = {
    (target.kind, target.architecture): target
    for target in (
        HOPPER,
        BLACKWELL_SM100,
        BLACKWELL_SM120,
        CDNA2_GFX90A,
        CDNA3_GFX942,
        CDNA4_GFX950,
    )
}


def resolve_target(kind: str, architecture: str) -> TargetSpec | None:
    return TARGETS.get((kind, architecture))


__all__ = [
    "BLACKWELL_SM100",
    "BLACKWELL_SM120",
    "CDNA2_GFX90A",
    "CDNA3_GFX942",
    "CDNA4_GFX950",
    "HOPPER",
    "TARGETS",
    "TargetRequirement",
    "TargetSpec",
    "resolve_target",
]
