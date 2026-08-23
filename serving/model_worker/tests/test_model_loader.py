# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from threading import Event, Thread

import pytest

from serving.model_worker import LoadedModel, ModelRegistry, WorkerLimits
from serving.model_worker.config import MAXIMUM_LOADED_MODELS

D = "sha256:" + "1" * 64


def digest(index: int) -> str:
    return f"sha256:{index:064x}"


class Loader:
    def __init__(self) -> None:
        self.loads = 0

    def load(self, bundle_digest: str) -> LoadedModel:
        self.loads += 1
        return LoadedModel(bundle_digest, object())


def test_registry_loads_content_addressed_model_once() -> None:
    loader = Loader()
    registry = ModelRegistry(loader)
    assert registry.get_or_load(D) is registry.get_or_load(D)
    assert loader.loads == 1


def test_registry_evicts_instead_of_retaining_every_bundle_digest() -> None:
    """A registry fed distinct digests must stay bounded, not grow to the request count.

    Model bundle digests arrive on the request, so an unbounded registry is an
    out-of-memory kill under a rolling deployment or a hostile caller.
    """
    capacity = WorkerLimits().maximum_loaded_models
    loader = Loader()
    registry = ModelRegistry(loader)
    for index in range(capacity * 4):
        registry.get_or_load(digest(index))
    assert loader.loads == capacity * 4
    assert len(registry) == capacity


def test_registry_evicts_the_least_recently_used_bundle() -> None:
    loader = Loader()
    registry = ModelRegistry(loader, capacity=2)
    first = registry.get_or_load(digest(1))
    registry.get_or_load(digest(2))
    # Re-reading the first bundle makes the second one the eviction candidate.
    assert registry.get_or_load(digest(1)) is first
    registry.get_or_load(digest(3))
    assert len(registry) == 2
    assert registry.get_or_load(digest(1)) is first
    assert loader.loads == 3
    registry.get_or_load(digest(2))
    assert loader.loads == 4


def test_registry_capacity_is_validated() -> None:
    for capacity in (0, -1, True, MAXIMUM_LOADED_MODELS + 1):
        with pytest.raises(ValueError):
            ModelRegistry(Loader(), capacity=capacity)
    with pytest.raises(ValueError):
        # A non-integral capacity out of a parsed deployment config must not slip
        # past the range check and leave the eviction trigger unreachable.
        ModelRegistry(Loader(), capacity=2.0)  # type: ignore[arg-type]
    assert ModelRegistry(Loader(), capacity=MAXIMUM_LOADED_MODELS).capacity == (
        MAXIMUM_LOADED_MODELS
    )


def test_registry_rejects_a_loader_that_returns_another_bundle() -> None:
    class Wrong:
        def load(self, bundle_digest: str) -> LoadedModel:
            return LoadedModel(digest(9), object())

    with pytest.raises(ValueError):
        ModelRegistry(Wrong()).get_or_load(D)


def test_a_cold_load_does_not_block_a_resident_cache_hit() -> None:
    """A multi-gigabyte cold load must not stall traffic for already-resident bundles."""
    loading = Event()
    finish = Event()
    hits: list[LoadedModel] = []

    class Slow:
        def load(self, bundle_digest: str) -> LoadedModel:
            if bundle_digest == digest(2):
                loading.set()
                finish.wait(30)
            return LoadedModel(bundle_digest, object())

    registry = ModelRegistry(Slow(), capacity=4)
    resident = registry.get_or_load(digest(1))
    cold = Thread(target=registry.get_or_load, args=(digest(2),), daemon=True)
    cold.start()
    try:
        assert loading.wait(30)
        hit = Thread(target=lambda: hits.append(registry.get_or_load(digest(1))), daemon=True)
        hit.start()
        hit.join(timeout=30)
        assert hits == [resident]
    finally:
        finish.set()
        cold.join(timeout=30)
    assert len(registry) == 2


def test_registry_does_not_deadlock_on_a_loader_that_resolves_through_it() -> None:
    class Chaining:
        def load(self, bundle_digest: str) -> LoadedModel:
            if bundle_digest != digest(1):
                registry.get_or_load(digest(1))
            return LoadedModel(bundle_digest, object())

    registry = ModelRegistry(Chaining(), capacity=4)
    worker = Thread(target=registry.get_or_load, args=(digest(2),), daemon=True)
    worker.start()
    worker.join(timeout=30)
    assert not worker.is_alive()
    assert len(registry) == 2


def test_worker_limits_reject_an_out_of_bounds_loaded_model_ceiling() -> None:
    with pytest.raises(ValueError):
        WorkerLimits(maximum_loaded_models=0).validate()
    with pytest.raises(ValueError):
        WorkerLimits(maximum_loaded_models=MAXIMUM_LOADED_MODELS + 1).validate()
    WorkerLimits(maximum_loaded_models=MAXIMUM_LOADED_MODELS).validate()
