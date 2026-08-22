# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Architecture defaults are baselines, not promoted winners."""

from kernels.providers.tilelang.attention.schedules import (
    BLACKWELL_FLASH,
    CONSERVATIVE_FLASH,
    HOPPER_FLASH,
    FlashAttentionSchedule,
)


def baseline_schedule(architecture: str, dtype: str) -> FlashAttentionSchedule:
    template = {
        "sm_90": HOPPER_FLASH,
        "sm_100": BLACKWELL_FLASH,
        "sm_120": BLACKWELL_FLASH,
    }.get(architecture, CONSERVATIVE_FLASH)
    return FlashAttentionSchedule(
        template.block_m,
        template.block_n,
        template.threads,
        template.num_stages,
        dtype=dtype,
    )
