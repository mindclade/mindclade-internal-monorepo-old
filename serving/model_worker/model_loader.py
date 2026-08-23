# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model loading contracts; concrete PyTorch loaders live with model adapters."""

from __future__ import annotations

from collections import OrderedDict
from dataclasses import dataclass
from threading import Lock
from typing import Protocol

from .config import MAXIMUM_LOADED_MODELS, WorkerLimits


@dataclass(frozen=True, slots=True)
class LoadedModel:
    bundle_digest: str
    implementation: object


class ModelLoader(Protocol):
    def load(self, bundle_digest: str) -> LoadedModel: ...


class ModelRegistry:
    """Bounded least-recently-used registry of loaded model bundles.

    This previously held a plain dict that was never evicted from, so every
    distinct bundle digest the process ever saw stayed resident for its whole
    life. ``ModelWorker.execute`` keys this on ``batch.key.model_bundle_digest``,
    which is carried on the request, so a rolling deployment - or any caller that
    varies the digest - grew the registry without limit. Model weights are large
    enough that a handful of retained bundles is already an out-of-memory kill,
    and an unbounded queue of them is exactly what the repository forbids.

    Eviction drops the registry's reference rather than calling an unload hook.
    A concurrent ``ModelWorker.execute`` may still be running a batch against the
    evicted ``LoadedModel``; refcounting releases the weights once that last
    in-flight batch lets go of it, whereas an eager unload would free device
    memory underneath a live batch.

    ``capacity`` bounds the *resident* set. A cold load runs outside the lock, so
    while one is in flight the peak is ``capacity`` resident bundles plus the one
    being materialized, per concurrent load; ``ModelWorker`` bounds the number of
    concurrent callers. Holding the lock across the load instead would make one
    multi-gigabyte cold load stall every cache hit for every other model, and
    would deadlock outright on a loader that resolves a base bundle through this
    same registry.
    """

    def __init__(self, loader: ModelLoader, *, capacity: int | None = None) -> None:
        resolved = WorkerLimits().maximum_loaded_models if capacity is None else capacity
        if (
            isinstance(resolved, bool)
            or not isinstance(resolved, int)
            or not 1 <= resolved <= MAXIMUM_LOADED_MODELS
        ):
            raise ValueError("model registry capacity is outside bounds")
        self._capacity = resolved
        self._loader = loader
        self._models: OrderedDict[str, LoadedModel] = OrderedDict()
        self._lock = Lock()

    @property
    def capacity(self) -> int:
        return self._capacity

    def get_or_load(self, bundle_digest: str) -> LoadedModel:
        if not bundle_digest.startswith("sha256:") or len(bundle_digest) != 71:
            raise ValueError("model bundle digest is invalid")
        resident = self._resident(bundle_digest)
        if resident is not None:
            return resident
        model = self._loader.load(bundle_digest)
        if model.bundle_digest != bundle_digest:
            raise ValueError("model loader returned a different bundle digest")
        with self._lock:
            # Two callers can race on one cold digest because the load runs
            # unlocked. The first publication wins and the loser's copy is
            # dropped here rather than displacing a bundle that live batches are
            # already holding.
            published = self._models.get(bundle_digest)
            if published is not None:
                self._models.move_to_end(bundle_digest)
                return published
            if len(self._models) >= self._capacity:
                self._models.popitem(last=False)
            self._models[bundle_digest] = model
            return model

    def _resident(self, bundle_digest: str) -> LoadedModel | None:
        with self._lock:
            model = self._models.get(bundle_digest)
            if model is None:
                return None
            self._models.move_to_end(bundle_digest)
            return model

    def __len__(self) -> int:
        with self._lock:
            return len(self._models)
