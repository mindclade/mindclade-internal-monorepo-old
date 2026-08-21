# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Byte-bounded LRU cache for provider-owned KV values."""

from collections import OrderedDict
from dataclasses import dataclass
from threading import Lock


@dataclass(frozen=True, slots=True)
class CacheEntry:
    value: object
    size_bytes: int


class KVCache:
    def __init__(self, capacity_bytes: int) -> None:
        if isinstance(capacity_bytes, bool) or capacity_bytes <= 0:
            raise ValueError("KV cache capacity must be positive")
        self._capacity = capacity_bytes
        self._size = 0
        self._values: OrderedDict[str, CacheEntry] = OrderedDict()
        self._lock = Lock()

    def put(self, key: str, value: object, size_bytes: int) -> tuple[str, ...]:
        if (
            not key
            or len(key) > 256
            or isinstance(size_bytes, bool)
            or not 1 <= size_bytes <= self._capacity
        ):
            raise ValueError("KV cache entry is invalid")
        evicted: list[str] = []
        with self._lock:
            previous = self._values.pop(key, None)
            if previous is not None:
                self._size -= previous.size_bytes
            while self._values and self._size + size_bytes > self._capacity:
                name, entry = self._values.popitem(last=False)
                self._size -= entry.size_bytes
                evicted.append(name)
            self._values[key] = CacheEntry(value, size_bytes)
            self._size += size_bytes
        return tuple(evicted)

    def get(self, key: str) -> object | None:
        with self._lock:
            entry = self._values.get(key)
            if entry is None:
                return None
            self._values.move_to_end(key)
            return entry.value
