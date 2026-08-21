# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded FIFO admission queue for provider-owned continuous batching."""

from collections import deque
from threading import Lock

from serving.model_worker.protocol import ModelRequest


class ContinuousBatchQueue:
    def __init__(self, capacity: int) -> None:
        if isinstance(capacity, bool) or not 1 <= capacity <= 65_536:
            raise ValueError("continuous batch queue capacity is outside bounds")
        self._capacity = capacity
        self._values: deque[ModelRequest] = deque()
        self._identifiers: set[str] = set()
        self._lock = Lock()

    def put(self, request: ModelRequest) -> None:
        request.validate()
        with self._lock:
            if request.request_id in self._identifiers:
                raise ValueError("continuous batch request is already queued")
            if len(self._values) == self._capacity:
                raise RuntimeError("continuous batch queue is full")
            self._values.append(request)
            self._identifiers.add(request.request_id)

    def take(self, maximum: int) -> tuple[ModelRequest, ...]:
        if isinstance(maximum, bool) or maximum <= 0:
            raise ValueError("continuous batch take limit must be positive")
        with self._lock:
            values = tuple(self._values.popleft() for _ in range(min(maximum, len(self._values))))
            self._identifiers.difference_update(value.request_id for value in values)
            return values
