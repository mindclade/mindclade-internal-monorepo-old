# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded deterministic gateway fake for contract tests."""

from __future__ import annotations

from collections.abc import Callable
from threading import Lock

from serving.contracts import InferenceRequest, InferenceResult


class FakeGateway:
    def __init__(
        self,
        handler: Callable[[InferenceRequest], InferenceResult],
        *,
        maximum_calls: int = 10_000,
    ) -> None:
        if not callable(handler):
            raise ValueError("fake gateway handler must be callable")
        if isinstance(maximum_calls, bool) or not 1 <= maximum_calls <= 1_000_000:
            raise ValueError("fake gateway call limit is outside bounds")
        self._handler = handler
        self._maximum_calls = maximum_calls
        self._calls: list[str] = []
        self._lock = Lock()

    def infer(self, request: InferenceRequest, *, now_unix_millis: int) -> InferenceResult:
        request.validate(now_unix_millis)
        with self._lock:
            if len(self._calls) >= self._maximum_calls:
                raise RuntimeError("fake gateway call history is full")
            self._calls.append(request.request_id)
        result = self._handler(request)
        result.validate()
        if result.request_id != request.request_id:
            raise RuntimeError("fake gateway handler returned another request id")
        return result

    @property
    def calls(self) -> tuple[str, ...]:
        with self._lock:
            return tuple(self._calls)
