# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Bounded, thread-safe cache of compiled TileLang kernel callables."""

from __future__ import annotations

from collections import OrderedDict
from collections.abc import Callable, Hashable
from threading import RLock
from typing import Any


class CompiledKernelCache:
    def __init__(self, max_entries: int = 64) -> None:
        if max_entries <= 0:
            raise ValueError("max_entries must be positive")
        self._max_entries = max_entries
        self._lock = RLock()
        self._entries: OrderedDict[Hashable, Any] = OrderedDict()

    def get_or_compile(self, key: Hashable, compile_kernel: Callable[[], Any]) -> Any:
        with self._lock:
            existing = self._entries.get(key)
            if existing is not None:
                self._entries.move_to_end(key)
                return existing
        compiled = compile_kernel()
        with self._lock:
            winner = self._entries.get(key)
            if winner is not None:
                self._entries.move_to_end(key)
                return winner
            self._entries[key] = compiled
            self._entries.move_to_end(key)
            while len(self._entries) > self._max_entries:
                self._entries.popitem(last=False)
            return compiled

    def clear(self) -> None:
        with self._lock:
            self._entries.clear()

    def __len__(self) -> int:
        with self._lock:
            return len(self._entries)
