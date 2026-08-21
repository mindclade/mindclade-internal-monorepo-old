# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thread-safe byte reservation ledger for model-local memory."""

from dataclasses import dataclass
from threading import Lock


@dataclass(frozen=True, slots=True)
class MemorySnapshot:
    capacity_bytes: int
    reserved_bytes: int


class MemoryLedger:
    def __init__(self, capacity_bytes: int) -> None:
        if isinstance(capacity_bytes, bool) or not 1 <= capacity_bytes <= 2**63 - 1:
            raise ValueError("memory capacity is outside bounds")
        self._capacity = capacity_bytes
        self._reserved = 0
        self._lock = Lock()

    def reserve(self, size_bytes: int) -> bool:
        if isinstance(size_bytes, bool) or size_bytes <= 0:
            raise ValueError("memory reservation must be positive")
        with self._lock:
            if self._reserved + size_bytes > self._capacity:
                return False
            self._reserved += size_bytes
            return True

    def release(self, size_bytes: int) -> None:
        if isinstance(size_bytes, bool) or size_bytes <= 0:
            raise ValueError("memory release must be positive")
        with self._lock:
            if size_bytes > self._reserved:
                raise ValueError("memory release exceeds reservation")
            self._reserved -= size_bytes

    def snapshot(self) -> MemorySnapshot:
        with self._lock:
            return MemorySnapshot(self._capacity, self._reserved)
