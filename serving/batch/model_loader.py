# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded least-recently-used cache for immutable model bundles."""

from __future__ import annotations

from collections import OrderedDict
from threading import Lock
from typing import Protocol, TypeVar

ModelT = TypeVar("ModelT")


class Loader(Protocol[ModelT]):
    def load(self, digest: str) -> ModelT: ...
    def unload(self, model: ModelT) -> None: ...


class ModelCache:
    def __init__(self, capacity: int, loader: Loader[ModelT]) -> None:
        if isinstance(capacity, bool) or not 1 <= capacity <= 128:
            raise ValueError("model cache capacity is outside bounds")
        self._capacity = capacity
        self._loader = loader
        self._models: OrderedDict[str, ModelT] = OrderedDict()
        self._closed = False
        self._lock = Lock()

    def get(self, digest: str) -> ModelT:
        if not digest.startswith("sha256:") or len(digest) != 71:
            raise ValueError("model digest is invalid")
        evicted: ModelT | None = None
        with self._lock:
            if self._closed:
                raise RuntimeError("model cache is closed")
            cached = self._models.get(digest)
            if cached is not None:
                self._models.move_to_end(digest)
                return cached
            model = self._loader.load(digest)
            if len(self._models) == self._capacity:
                _, evicted = self._models.popitem(last=False)
            self._models[digest] = model
        if evicted is not None:
            self._loader.unload(evicted)
        return model

    def close(self) -> None:
        with self._lock:
            if self._closed:
                return
            self._closed = True
            models = tuple(self._models.values())
            self._models.clear()
        for model in models:
            self._loader.unload(model)
