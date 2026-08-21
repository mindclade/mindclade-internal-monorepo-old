# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded rollout worker over an injected actor."""

from __future__ import annotations

from libs.python.worker_runtime import CancellationToken

from .actor import Actor, RolloutRequest
from .batching import group_requests
from .health import RolloutHealth
from .policy_sync import PolicySynchronizer
from .trajectory import Trajectory


class RolloutWorker:
    def __init__(
        self, actor: Actor, synchronizer: PolicySynchronizer, maximum_batch_size: int = 128
    ) -> None:
        if not callable(getattr(actor, "generate", None)):
            raise ValueError("rollout actor is invalid")
        if isinstance(maximum_batch_size, bool) or not 1 <= maximum_batch_size <= 4096:
            raise ValueError("rollout batch size is outside bounds")
        self._actor = actor
        self._synchronizer = synchronizer
        self._maximum_batch_size = maximum_batch_size
        self._ready = False
        self._draining = False

    def ready(self, *, now_unix_millis: int) -> None:
        if self._synchronizer.current(now_unix_millis=now_unix_millis) is None:
            raise RuntimeError("rollout worker has no fresh policy snapshot")
        self._ready = True

    def execute(
        self,
        requests: tuple[RolloutRequest, ...],
        *,
        now_unix_millis: int,
        cancellation: CancellationToken | None = None,
    ) -> tuple[Trajectory, ...]:
        if not self._ready or self._draining:
            raise RuntimeError("rollout worker is not accepting work")
        snapshot = self._synchronizer.current(now_unix_millis=now_unix_millis)
        if snapshot is None:
            self._ready = False
            raise RuntimeError("rollout policy snapshot is stale")
        identifiers: set[str] = set()
        for request in requests:
            request.validate(now_unix_millis=now_unix_millis)
            if request.policy_digest != snapshot.policy_digest:
                raise ValueError("rollout request policy is not the active policy")
            if request.trajectory_id in identifiers:
                raise ValueError("rollout request ids must be unique")
            identifiers.add(request.trajectory_id)
        token = cancellation or CancellationToken()
        trajectories: list[Trajectory] = []
        for batch in group_requests(requests, self._maximum_batch_size):
            if token.is_cancelled:
                raise RuntimeError("rollout execution was canceled")
            generated = self._actor.generate(batch, token)
            expected = tuple(request.trajectory_id for request in batch)
            actual = tuple(trajectory.trajectory_id for trajectory in generated)
            if actual != expected:
                raise RuntimeError("rollout actor violated order/cardinality")
            for request, trajectory in zip(batch, generated, strict=True):
                trajectory.validate()
                if (
                    trajectory.policy_digest != request.policy_digest
                    or trajectory.environment_digest != request.environment_digest
                    or trajectory.seed != request.seed
                ):
                    raise RuntimeError("rollout actor violated trajectory provenance")
            trajectories.extend(generated)
        return tuple(trajectories)

    def drain(self) -> None:
        self._draining = True
        self._ready = False

    def health(self, *, now_unix_millis: int) -> RolloutHealth:
        snapshot = self._synchronizer.current(now_unix_millis=now_unix_millis)
        return RolloutHealth(
            self._ready and snapshot is not None,
            self._draining,
            None if snapshot is None else snapshot.policy_digest,
        )
