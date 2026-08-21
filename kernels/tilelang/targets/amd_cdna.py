# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""AMD CDNA targets with no NVIDIA-only capability leakage."""

from kernels.api.capabilities import DeviceCapabilities
from kernels.tilelang.targets.common import TargetSpec


def _cdna(architecture: str, shared_memory: int) -> TargetSpec:
    return TargetSpec(
        kind="hip",
        architecture=architecture,
        capabilities=DeviceCapabilities(
            target="hip",
            architecture=architecture,
            warp_size=64,
            max_threads_per_block=1024,
            shared_memory_per_block=shared_memory,
            tensor_core_dtypes=frozenset({"float16", "bfloat16", "int8"}),
            supports_async_copy=False,
        ),
        tilelang_target={"kind": "hip", "mcpu": architecture},
    )


CDNA2_GFX90A = _cdna("gfx90a", 65_536)
CDNA3_GFX942 = _cdna("gfx942", 65_536)
CDNA4_GFX950 = _cdna("gfx950", 163_840)
