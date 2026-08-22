# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, disjoint rank sharding for bounded qualification batches."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Final

import torch

from libs.python.errors import InvalidArgument
from training.contracts import SupervisedBatch

SUPPORTED_SHARD_WORLD_SIZES: Final = frozenset({1, 2, 8})
MAXIMUM_DATA_POSITION: Final = (1 << 63) - 1


@dataclass(frozen=True, slots=True)
class ShardedSupervisedBatch:
    """One rank's explicit view of a globally ordered batch."""

    batch: SupervisedBatch
    global_sample_ids: tuple[int, ...]
    next_global_position: int

    def __post_init__(self) -> None:
        if not isinstance(self.batch, SupervisedBatch):
            raise InvalidArgument(
                "sharded batch payload must be SupervisedBatch",
                reason="distributed_shard_batch",
            )
        if len(self.global_sample_ids) != self.batch.batch_size:
            raise InvalidArgument(
                "sharded sample IDs must match the local batch size",
                reason="distributed_shard_ids",
            )


def shard_supervised_batch(
    batch: SupervisedBatch,
    *,
    rank: int,
    world_size: int,
    global_position: int = 0,
) -> ShardedSupervisedBatch:
    """Partition a global batch by stable strided sample ID without duplication.

    This is a deliberately narrow qualification fixture. It materializes an
    explicit local batch and does not claim streaming-loader or worker resume
    semantics.
    """

    if not isinstance(batch, SupervisedBatch):
        raise InvalidArgument(
            "global batch must be SupervisedBatch",
            reason="distributed_shard_batch",
        )
    if (
        isinstance(world_size, bool)
        or not isinstance(world_size, int)
        or world_size not in SUPPORTED_SHARD_WORLD_SIZES
    ):
        raise InvalidArgument(
            "shard world size must be an approved value (1, 2, or 8)",
            reason="distributed_shard_world",
        )
    if isinstance(rank, bool) or not isinstance(rank, int) or not 0 <= rank < world_size:
        raise InvalidArgument(
            "shard rank is outside its world",
            reason="distributed_shard_rank",
        )
    if (
        isinstance(global_position, bool)
        or not isinstance(global_position, int)
        or not 0 <= global_position <= MAXIMUM_DATA_POSITION
        or batch.batch_size > MAXIMUM_DATA_POSITION - global_position
    ):
        raise InvalidArgument(
            "global data position is outside bounds",
            reason="distributed_shard_position",
        )
    if batch.batch_size < world_size:
        raise InvalidArgument(
            "qualification batch must assign at least one sample to every rank",
            reason="distributed_shard_empty_rank",
        )

    local_indices = tuple(range(rank, batch.batch_size, world_size))
    index = torch.tensor(local_indices, dtype=torch.int64, device=batch.device)
    local = SupervisedBatch(
        batch.inputs.index_select(0, index),
        batch.targets.index_select(0, index),
    )
    sample_ids = tuple(global_position + offset for offset in local_indices)
    return ShardedSupervisedBatch(
        local,
        sample_ids,
        global_position + batch.batch_size,
    )


__all__ = ["ShardedSupervisedBatch", "shard_supervised_batch"]
