# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Length-homogeneous trajectory batching without silent truncation."""

from __future__ import annotations

from .trajectory import Trajectory


def bucket_trajectories(
    trajectories: tuple[Trajectory, ...], *, maximum_steps: int
) -> tuple[tuple[Trajectory, ...], ...]:
    if isinstance(maximum_steps, bool) or not isinstance(maximum_steps, int) or maximum_steps < 1:
        raise ValueError("trajectory batch step limit is invalid")
    ordered = sorted(trajectories, key=lambda item: (len(item.steps), item.trajectory_id))
    batches: list[list[Trajectory]] = []
    used: list[int] = []
    for trajectory in ordered:
        length = len(trajectory.steps)
        if length > maximum_steps:
            raise ValueError("trajectory exceeds batch step limit")
        if batches and used[-1] + length <= maximum_steps:
            batches[-1].append(trajectory)
            used[-1] += length
        else:
            batches.append([trajectory])
            used.append(length)
    return tuple(tuple(batch) for batch in batches)
