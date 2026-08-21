# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from data.loaders.experience import ExperienceStep, PolicyVersion, ReplayBuffer, Trajectory

DIGESTS = tuple("sha256:" + character * 64 for character in "abcd")


def trajectory(identity: str) -> Trajectory:
    return Trajectory(
        identity,
        DIGESTS[0],
        PolicyVersion(DIGESTS[1], DIGESTS[2], DIGESTS[3]),
        (ExperienceStep(DIGESTS[0], 1, 0.5, True),),
    )


def test_replay_is_bounded_deduplicated_and_seeded() -> None:
    buffer = ReplayBuffer(2)
    buffer.append(trajectory("t-1"))
    buffer.append(trajectory("t-1"))
    assert len(buffer) == 1
    buffer.append(trajectory("t-2"))
    assert [item.trajectory_id for item in buffer.sample(2, seed=7)] == [
        item.trajectory_id for item in buffer.sample(2, seed=7)
    ]
