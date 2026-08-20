# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from kernels.providers.tilelang.moe.schedules import GroupedGemmSchedule

BASELINE_GROUPED_GEMM = GroupedGemmSchedule(
    64,
    64,
    32,
    128,
    2,
    "bfloat16",
    "bfloat16",
)
