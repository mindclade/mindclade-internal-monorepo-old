# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated streaming prefetch policy."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class PrefetchPolicy:
    batches_per_worker: int
    memory_budget_bytes: int

    def __post_init__(self) -> None:
        if (
            isinstance(self.batches_per_worker, bool)
            or not isinstance(self.batches_per_worker, int)
            or not 1 <= self.batches_per_worker <= 128
            or isinstance(self.memory_budget_bytes, bool)
            or not isinstance(self.memory_budget_bytes, int)
            or not 1 <= self.memory_budget_bytes <= 2**63 - 1
        ):
            raise ValueError("streaming prefetch policy is outside bounds")
