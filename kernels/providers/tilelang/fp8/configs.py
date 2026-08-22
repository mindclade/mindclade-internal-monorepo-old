# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Conservative architecture defaults for scaled GEMM."""

from kernels.providers.tilelang.fp8.schedules import GemmSchedule


def baseline_schedule(
    architecture: str, input_dtype: str, output_dtype: str = "bfloat16"
) -> GemmSchedule:
    if architecture in {"sm_100", "sm_120"}:
        return GemmSchedule(128, 128, 64, 256, 3, input_dtype, output_dtype)
    if architecture == "sm_90":
        return GemmSchedule(128, 128, 64, 256, 3, input_dtype, output_dtype)
    return GemmSchedule(
        64,
        64,
        32 if not input_dtype.startswith("float8") else 64,
        128,
        1,
        input_dtype,
        output_dtype,
    )
