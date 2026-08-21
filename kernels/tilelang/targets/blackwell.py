# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Blackwell target declarations; qualification remains architecture-specific."""

from kernels.tilelang.targets.common import TargetSpec, cuda_target


def _blackwell(architecture: str) -> TargetSpec:
    return cuda_target(
        architecture,
        shared_memory=232_448,
        dtypes=frozenset(
            {
                "float16",
                "bfloat16",
                "float8_e4m3fn",
                "float8_e5m2",
                "float4_e2m1fn_x2",
                "int8",
            }
        ),
        tma=True,
        wgmma=True,
        tmem=True,
    )


BLACKWELL_SM100 = _blackwell("sm_100")
BLACKWELL_SM120 = _blackwell("sm_120")
