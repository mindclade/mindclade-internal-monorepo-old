# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded immutable warmup plan."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class WarmupCase:
    shape_bucket: str
    repetitions: int = 1

    def __post_init__(self) -> None:
        if not self.shape_bucket or len(self.shape_bucket) > 128:
            raise ValueError("warmup shape bucket is invalid")
        if isinstance(self.repetitions, bool) or not 1 <= self.repetitions <= 1000:
            raise ValueError("warmup repetitions are outside bounds")


@dataclass(frozen=True, slots=True)
class WarmupPlan:
    cases: tuple[WarmupCase, ...]

    def __post_init__(self) -> None:
        if not self.cases or len(self.cases) > 1024:
            raise ValueError("warmup case count is outside bounds")
        if len({case.shape_bucket for case in self.cases}) != len(self.cases):
            raise ValueError("warmup shape buckets must be unique")
