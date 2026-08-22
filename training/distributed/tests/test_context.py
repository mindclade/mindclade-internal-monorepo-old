# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pure validation coverage for torchrun identity and deterministic sharding."""

from __future__ import annotations

import pytest
import torch

from training.contracts import SupervisedBatch
from training.distributed import (
    DistributedConfig,
    DistributedContext,
    TorchrunEnvironment,
    shard_supervised_batch,
)


def test_torchrun_environment_is_launcher_owned_and_bounded() -> None:
    environment = TorchrunEnvironment.from_environ(
        {"RANK": "1", "LOCAL_RANK": "1", "WORLD_SIZE": "2", "LOCAL_WORLD_SIZE": "2"}
    )

    assert environment == TorchrunEnvironment(
        rank=1, local_rank=1, world_size=2, local_world_size=2
    )
    assert DistributedConfig(backend="gloo", timeout_seconds=60).timeout_seconds == 60


@pytest.mark.parametrize(
    "environ",
    [
        {},
        {"RANK": "-1", "LOCAL_RANK": "0", "WORLD_SIZE": "2", "LOCAL_WORLD_SIZE": "2"},
        {"RANK": "0", "LOCAL_RANK": "0", "WORLD_SIZE": "1", "LOCAL_WORLD_SIZE": "1"},
        {"RANK": "2", "LOCAL_RANK": "0", "WORLD_SIZE": "2", "LOCAL_WORLD_SIZE": "2"},
        {"RANK": "1", "LOCAL_RANK": "0", "WORLD_SIZE": "2", "LOCAL_WORLD_SIZE": "2"},
        {"RANK": "0", "LOCAL_RANK": "0", "WORLD_SIZE": "4", "LOCAL_WORLD_SIZE": "4"},
        {"RANK": "0", "LOCAL_RANK": "0", "WORLD_SIZE": "8", "LOCAL_WORLD_SIZE": "4"},
    ],
)
def test_torchrun_environment_rejects_missing_or_inconsistent_values(
    environ: dict[str, str],
) -> None:
    with pytest.raises(ValueError, match="torchrun"):
        TorchrunEnvironment.from_environ(environ)


def test_backend_and_world_size_matrix_is_fail_closed() -> None:
    world_two = TorchrunEnvironment(rank=0, local_rank=0, world_size=2, local_world_size=2)
    world_eight = TorchrunEnvironment(rank=0, local_rank=0, world_size=8, local_world_size=8)

    with pytest.raises(ValueError, match="approved world size"):
        DistributedContext(world_two, "nccl", torch.device("cuda", 0))
    with pytest.raises(ValueError, match="approved world size"):
        DistributedContext(world_eight, "gloo", torch.device("cpu"))


def test_deterministic_shards_are_disjoint_exhaustive_and_cursor_stable() -> None:
    inputs = torch.arange(7, dtype=torch.float32).reshape(-1, 1)
    global_batch = SupervisedBatch(inputs, (inputs * 3.0) - 1.0)

    shards = tuple(
        shard_supervised_batch(global_batch, rank=rank, world_size=2, global_position=100)
        for rank in range(2)
    )

    assert shards[0].global_sample_ids == (100, 102, 104, 106)
    assert shards[1].global_sample_ids == (101, 103, 105)
    assert {sample_id for shard in shards for sample_id in shard.global_sample_ids} == set(
        range(100, 107)
    )
    assert all(shard.next_global_position == 107 for shard in shards)
    reconstructed = torch.empty_like(inputs)
    for shard in shards:
        offsets = torch.tensor([sample_id - 100 for sample_id in shard.global_sample_ids])
        reconstructed.index_copy_(0, offsets, shard.batch.inputs)
    torch.testing.assert_close(reconstructed, inputs, rtol=0.0, atol=0.0)


def test_sharding_rejects_a_batch_that_would_leave_an_empty_rank() -> None:
    batch = SupervisedBatch(torch.ones(1, 1), torch.ones(1, 1))

    with pytest.raises(ValueError, match="at least one sample"):
        shard_supervised_batch(batch, rank=0, world_size=2)


def test_sharding_rejects_an_unapproved_world_size() -> None:
    batch = SupervisedBatch(torch.ones(4, 1), torch.ones(4, 1))

    with pytest.raises(ValueError, match="approved value"):
        shard_supervised_batch(batch, rank=0, world_size=4)
