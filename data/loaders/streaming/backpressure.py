# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded queue policy for streaming adapters."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class BackpressurePolicy:
    maximum_queued_batches: int
    put_timeout_seconds: float

    def __post_init__(self) -> None:
        if (
            isinstance(self.maximum_queued_batches, bool)
            or not isinstance(self.maximum_queued_batches, int)
            or not 1 <= self.maximum_queued_batches <= 1024
            or isinstance(self.put_timeout_seconds, bool)
            or not isinstance(self.put_timeout_seconds, int | float)
            or not 0 < self.put_timeout_seconds <= 3600
        ):
            raise ValueError("streaming backpressure policy is outside bounds")
