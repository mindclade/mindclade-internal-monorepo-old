# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from serving.batch.cancellation import CancellationRegistry
from serving.batch.model_loader import ModelCache
from serving.batch.queue import JobQueue
from serving.batch.tests.test_batching import job, request


def test_cancellation_is_idempotent_for_callers_and_bounded_for_owners() -> None:
    registry = CancellationRegistry(1)
    token = registry.register("job-1")
    assert registry.cancel("job-1")
    assert token.is_cancelled
    assert not registry.cancel("unknown")
    with pytest.raises(ValueError, match="already"):
        registry.register("job-1")
    registry.release("job-1")
    assert not registry.cancel("job-1")


def test_queue_is_bounded_and_orders_by_deadline_then_submission() -> None:
    queue = JobQueue(2)
    later = job(request("later"), identifier="later", deadline=9_000)
    sooner = job(request("sooner"), identifier="sooner", deadline=8_000)
    queue.put(later)
    queue.put(sooner)
    with pytest.raises(RuntimeError, match="full"):
        queue.put(job(request("third"), identifier="third"))
    assert queue.get_nowait() is sooner
    assert queue.get_nowait() is later
    assert queue.get_nowait() is None


def test_model_cache_evicts_least_recently_used_and_unloads_on_close() -> None:
    class Loader:
        def __init__(self) -> None:
            self.unloaded: list[str] = []

        def load(self, digest: str) -> str:
            return digest

        def unload(self, model: str) -> None:
            self.unloaded.append(model)

    first = "sha256:" + "1" * 64
    second = "sha256:" + "2" * 64
    loader = Loader()
    cache = ModelCache(1, loader)
    assert cache.get(first) == first
    assert cache.get(second) == second
    assert loader.unloaded == [first]
    cache.close()
    assert loader.unloaded == [first, second]
    cache.close()
    with pytest.raises(RuntimeError, match="closed"):
        cache.get(first)
