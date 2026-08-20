# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Hopper target declarations and WGMMA/TMA capability boundary."""

from kernels.tilelang.targets.common import cuda_target

HOPPER = cuda_target(
    "sm_90",
    shared_memory=227_328,
    dtypes=frozenset(
        {"float16", "bfloat16", "float8_e4m3fn", "float8_e5m2", "int8"}
    ),
    tma=True,
    wgmma=True,
)
