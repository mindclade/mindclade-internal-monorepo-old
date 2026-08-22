# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Padded expert-major grouped-GEMM schedule contract."""

from kernels.providers.tilelang.fp8.schedules import GemmSchedule

GroupedGemmSchedule = GemmSchedule


def candidate_schedules(dtype: str) -> tuple[GroupedGemmSchedule, ...]:
    return tuple(
        GroupedGemmSchedule(m, n, 32, threads, stages, dtype, dtype)
        for m, n, threads, stages in (
            (32, 64, 128, 1),
            (64, 64, 128, 2),
            (64, 128, 256, 3),
        )
    )
