# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Combined distributed-rank and DataLoader-worker partition."""

from __future__ import annotations


def worker_indices(
    length: int,
    *,
    rank: int,
    world_size: int,
    worker_id: int,
    num_workers: int,
) -> tuple[int, ...]:
    values = (length, rank, world_size, worker_id, num_workers)
    if any(isinstance(value, bool) or not isinstance(value, int) for value in values):
        raise ValueError("worker partition values must be integers")
    if (
        length < 0
        or world_size < 1
        or num_workers < 1
        or not 0 <= rank < world_size
        or not 0 <= worker_id < num_workers
    ):
        raise ValueError("worker partition bounds are invalid")
    shard = rank * num_workers + worker_id
    shard_count = world_size * num_workers
    return tuple(range(shard, length, shard_count))
