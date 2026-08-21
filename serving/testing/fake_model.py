# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic model fake that preserves batch order and bundle identity."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from threading import Lock

from libs.python.worker_runtime import CancellationToken
from serving.batch import BatchSlice
from serving.contracts import InferenceResult


@dataclass(frozen=True, slots=True)
class ModelCall:
    request_ids: tuple[str, ...]


class FakeModel:
    def __init__(self, *, output_bytes: int = 16) -> None:
        if isinstance(output_bytes, bool) or not 1 <= output_bytes <= 1 << 30:
            raise ValueError("fake model output size is outside bounds")
        self._output_bytes = output_bytes
        self._calls: list[ModelCall] = []
        self._lock = Lock()

    def execute(
        self, batch: BatchSlice, cancellation: CancellationToken
    ) -> tuple[InferenceResult, ...]:
        if cancellation.is_cancelled:
            raise RuntimeError("fake model execution was canceled")
        request_ids = tuple(request.request_id for request in batch.requests)
        with self._lock:
            self._calls.append(ModelCall(request_ids))
        return tuple(
            InferenceResult(
                request.request_id,
                "sha256:"
                + hashlib.sha256(
                    request.request_key + request.model_bundle_digest.encode()
                ).hexdigest(),
                self._output_bytes,
                request.model_bundle_digest,
            )
            for request in batch.requests
        )

    @property
    def calls(self) -> tuple[ModelCall, ...]:
        with self._lock:
            return tuple(self._calls)
