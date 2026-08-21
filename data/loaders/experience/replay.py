# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded in-process replay buffer for tests and worker-local sampling."""

from __future__ import annotations

import random

from .trajectory import Trajectory


class ReplayBuffer:
    def __init__(self, capacity: int) -> None:
        if (
            isinstance(capacity, bool)
            or not isinstance(capacity, int)
            or not 1 <= capacity <= 10_000_000
        ):
            raise ValueError("replay capacity is outside bounds")
        self._capacity = capacity
        self._items: list[Trajectory] = []
        self._digests: set[str] = set()

    def append(self, trajectory: Trajectory) -> None:
        if not isinstance(trajectory, Trajectory):
            raise TypeError("replay values must be trajectories")
        if trajectory.digest in self._digests:
            return
        if len(self._items) == self._capacity:
            removed = self._items.pop(0)
            self._digests.remove(removed.digest)
        self._items.append(trajectory)
        self._digests.add(trajectory.digest)

    def sample(self, count: int, *, seed: int) -> tuple[Trajectory, ...]:
        if count > len(self._items) or count < 0:
            raise ValueError("replay sample count is outside available population")
        return tuple(random.Random(seed).sample(self._items, count))

    def __len__(self) -> int:
        return len(self._items)
