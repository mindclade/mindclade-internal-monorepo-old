# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from data.loaders.sharding import ShardCursor, assign, rank_indices, worker_indices

DIGEST = "sha256:" + "a" * 64


def test_rank_and_worker_partitions_are_disjoint_and_complete() -> None:
    ranks = [set(rank_indices(17, rank, 3)) for rank in range(3)]
    assert set.union(*ranks) == set(range(17))
    assert all(ranks[left].isdisjoint(ranks[right]) for left in range(3) for right in range(left))

    workers = [
        set(worker_indices(17, rank=rank, world_size=2, worker_id=worker, num_workers=2))
        for rank in range(2)
        for worker in range(2)
    ]
    assert set.union(*workers) == set(range(17))
    assert sum(len(partition) for partition in workers) == 17


def test_assignment_and_resume_are_stable_and_content_bound() -> None:
    first = assign(("shard-1", "shard-2"), ("worker-a", "worker-b"))
    second = assign(("shard-2", "shard-1"), ("worker-b", "worker-a"))
    assert first == second
    assert ShardCursor(DIGEST, "shard-1", 4).advance(3).next_record == 7
