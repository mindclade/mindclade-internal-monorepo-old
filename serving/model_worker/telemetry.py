# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Low-cardinality model-worker counters."""

from dataclasses import dataclass
from threading import Lock


@dataclass(frozen=True, slots=True)
class WorkerMetrics:
    admitted: int
    completed: int
    rejected: int
    failed: int


class Telemetry:
    def __init__(self) -> None:
        self._values = [0, 0, 0, 0]
        self._lock = Lock()

    def increment(self, outcome: str) -> None:
        indexes = {"admitted": 0, "completed": 1, "rejected": 2, "failed": 3}
        if outcome not in indexes:
            raise ValueError("model-worker outcome is invalid")
        with self._lock:
            self._values[indexes[outcome]] += 1

    def snapshot(self) -> WorkerMetrics:
        with self._lock:
            return WorkerMetrics(*self._values)
