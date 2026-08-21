# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded immutable policy cache."""

from __future__ import annotations

from collections import OrderedDict
from threading import Lock
from typing import Protocol, TypeVar

PolicyT = TypeVar("PolicyT")


class PolicyLoader(Protocol[PolicyT]):
    def load(self, digest: str) -> PolicyT: ...
    def unload(self, policy: PolicyT) -> None: ...


class PolicyCache:
    def __init__(self, capacity: int, loader: PolicyLoader[PolicyT]) -> None:
        if isinstance(capacity, bool) or not 1 <= capacity <= 128:
            raise ValueError("policy cache capacity is outside bounds")
        self._capacity = capacity
        self._loader = loader
        self._values: OrderedDict[str, PolicyT] = OrderedDict()
        self._closed = False
        self._lock = Lock()

    def get(self, digest: str) -> PolicyT:
        if not digest.startswith("sha256:") or len(digest) != 71:
            raise ValueError("policy digest is invalid")
        evicted: PolicyT | None = None
        with self._lock:
            if self._closed:
                raise RuntimeError("policy cache is closed")
            value = self._values.get(digest)
            if value is not None:
                self._values.move_to_end(digest)
                return value
            value = self._loader.load(digest)
            if len(self._values) == self._capacity:
                _, evicted = self._values.popitem(last=False)
            self._values[digest] = value
        if evicted is not None:
            self._loader.unload(evicted)
        return value

    def close(self) -> None:
        with self._lock:
            if self._closed:
                return
            self._closed = True
            values = tuple(self._values.values())
            self._values.clear()
        for value in values:
            self._loader.unload(value)
