# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from serving.rollouts import (
    PolicyCache,
    PolicySnapshot,
    PolicySynchronizer,
    RolloutRequest,
    RolloutWorker,
    SamplingConfig,
    Trajectory,
    TrajectoryStep,
    categorical,
    derive_seed,
    group_requests,
)

POLICY = "sha256:" + "a" * 64
ENVIRONMENT = "sha256:" + "b" * 64
INPUT = "sha256:" + "c" * 64


def request(identifier: str, *, policy: str = POLICY, temperature: float = 1.0) -> RolloutRequest:
    return RolloutRequest(
        identifier, policy, ENVIRONMENT, INPUT, 7, 10_000, SamplingConfig(temperature=temperature)
    )


class Actor:
    def generate(self, requests, cancellation):
        return tuple(
            Trajectory(
                item.trajectory_id,
                item.policy_digest,
                item.environment_digest,
                item.seed,
                (TrajectoryStep(INPUT, b"action", 1.0, True),),
            )
            for item in requests
        )


def synchronizer() -> PolicySynchronizer:
    value = PolicySynchronizer()
    assert value.update(PolicySnapshot(1, POLICY, 20_000), now_unix_millis=1_000)
    return value


def test_seed_and_categorical_sampling_are_reproducible() -> None:
    seed = derive_seed(42, "trajectory-1", POLICY)
    assert seed == derive_seed(42, "trajectory-1", POLICY)
    assert categorical((0.1, 0.9), seed=seed) == categorical((0.1, 0.9), seed=seed)


def test_policy_updates_are_monotonic_and_expire_closed() -> None:
    value = synchronizer()
    assert not value.update(PolicySnapshot(1, POLICY, 30_000), now_unix_millis=1_000)
    assert value.current(now_unix_millis=19_999) is not None
    assert value.current(now_unix_millis=20_000) is None


def test_batching_groups_compatible_requests_stably() -> None:
    batches = group_requests((request("a"), request("b", temperature=0.5), request("c")), 2)
    assert [[item.trajectory_id for item in batch] for batch in batches] == [["a", "c"], ["b"]]


def test_worker_requires_fresh_active_policy_and_exact_actor_results() -> None:
    worker = RolloutWorker(Actor(), synchronizer(), maximum_batch_size=1)
    worker.ready(now_unix_millis=1_000)
    result = worker.execute((request("a"), request("b")), now_unix_millis=1_000)
    assert [item.trajectory_id for item in result] == ["a", "b"]
    with pytest.raises(ValueError, match="active policy"):
        worker.execute((request("wrong", policy="sha256:" + "d" * 64),), now_unix_millis=1_000)


def test_worker_rejects_actor_provenance_substitution() -> None:
    class BrokenActor:
        def generate(self, requests, cancellation):
            item = requests[0]
            return (
                Trajectory(
                    item.trajectory_id,
                    item.policy_digest,
                    "sha256:" + "d" * 64,
                    item.seed,
                    (TrajectoryStep(INPUT, b"action", 1.0, True),),
                ),
            )

    worker = RolloutWorker(BrokenActor(), synchronizer())
    worker.ready(now_unix_millis=1_000)
    with pytest.raises(RuntimeError, match="provenance"):
        worker.execute((request("a"),), now_unix_millis=1_000)


def test_trajectory_rejects_steps_after_terminal() -> None:
    step = TrajectoryStep(INPUT, b"action", 1.0, True)
    with pytest.raises(ValueError, match="after a terminal"):
        Trajectory("id", POLICY, ENVIRONMENT, 1, (step, step)).validate()


def test_policy_cache_unloads_resources_and_closes_terminally() -> None:
    class Loader:
        def __init__(self) -> None:
            self.unloaded = []

        def load(self, digest):
            return digest

        def unload(self, policy):
            self.unloaded.append(policy)

    loader = Loader()
    cache = PolicyCache(1, loader)
    assert cache.get(POLICY) == POLICY
    cache.close()
    assert loader.unloaded == [POLICY]
    with pytest.raises(RuntimeError, match="closed"):
        cache.get(POLICY)
