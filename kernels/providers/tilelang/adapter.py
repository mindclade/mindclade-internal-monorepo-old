# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded, fork-safe, single-flight cache of compiled TileLang callables."""

from __future__ import annotations

import os
from collections import OrderedDict
from collections.abc import Callable, Hashable
from concurrent.futures import Future
from threading import RLock
from typing import Any


class CompiledKernelCache:
    """Share one compilation per key and never retain a failed compilation."""

    def __init__(self, max_entries: int = 64) -> None:
        if max_entries <= 0:
            raise ValueError("max_entries must be positive")
        self._max_entries = max_entries
        self._lock = RLock()
        self._entries: OrderedDict[Hashable, Future[Any]] = OrderedDict()
        if hasattr(os, "register_at_fork"):
            os.register_at_fork(after_in_child=self.clear)

    def _evict_completed(self) -> None:
        while len(self._entries) > self._max_entries:
            victim = next(
                (key for key, future in self._entries.items() if future.done()),
                None,
            )
            if victim is None:
                return
            del self._entries[victim]

    def get_or_compile(self, key: Hashable, compile_kernel: Callable[[], Any]) -> Any:
        with self._lock:
            future = self._entries.get(key)
            owner = future is None
            if future is None:
                future = Future()
                self._entries[key] = future
            self._entries.move_to_end(key)

        if not owner:
            return future.result()

        try:
            compiled = compile_kernel()
        except BaseException as exc:
            future.set_exception(exc)
            with self._lock:
                if self._entries.get(key) is future:
                    del self._entries[key]
            raise

        future.set_result(compiled)
        with self._lock:
            if self._entries.get(key) is future:
                self._entries.move_to_end(key)
            self._evict_completed()
        return compiled

    def clear(self) -> None:
        with self._lock:
            self._entries.clear()

    def __len__(self) -> int:
        with self._lock:
            return len(self._entries)
