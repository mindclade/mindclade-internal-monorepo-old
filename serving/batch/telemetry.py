# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Low-cardinality process-local batch counters."""

from __future__ import annotations

from dataclasses import dataclass
from threading import Lock


@dataclass(frozen=True, slots=True)
class BatchMetrics:
    admitted: int
    completed: int
    failed: int
    canceled: int


class BatchTelemetry:
    def __init__(self) -> None:
        self._values = [0, 0, 0, 0]
        self._lock = Lock()

    def increment(self, outcome: str) -> None:
        indexes = {"admitted": 0, "completed": 1, "failed": 2, "canceled": 3}
        try:
            index = indexes[outcome]
        except KeyError as error:
            raise ValueError("batch telemetry outcome is invalid") from error
        with self._lock:
            self._values[index] += 1

    def snapshot(self) -> BatchMetrics:
        with self._lock:
            return BatchMetrics(*self._values)
