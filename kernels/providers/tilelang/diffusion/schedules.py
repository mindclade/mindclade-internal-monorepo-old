# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Bandwidth-bound diffusion epilogue launch schedules."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class DiffusionEpilogueSchedule:
    threads: int = 256
    vector_width: int = 4

    def __post_init__(self) -> None:
        if self.threads not in {128, 256, 512} or self.vector_width not in {1, 2, 4, 8}:
            raise ValueError("unsupported diffusion epilogue schedule")


def candidate_schedules() -> tuple[DiffusionEpilogueSchedule, ...]:
    return tuple(
        DiffusionEpilogueSchedule(threads, width)
        for threads, width in ((128, 2), (256, 4), (256, 8), (512, 4))
    )
