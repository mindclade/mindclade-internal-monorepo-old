# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from data.loaders.streaming import ShardedSampleStream, StreamCheckpoint, buffered_shuffle
from data.sample import Sample

DIGEST = "sha256:" + "a" * 64


def samples() -> tuple[Sample, ...]:
    return tuple(Sample(f"s-{index}", {"value": index}, DIGEST) for index in range(11))


def test_rank_streams_are_disjoint_complete_and_resumable() -> None:
    values = samples()
    partitions = [tuple(ShardedSampleStream(values, rank=rank, world_size=3)) for rank in range(3)]
    identities = [sample.sample_id for partition in partitions for sample in partition]
    assert sorted(identities) == sorted(sample.sample_id for sample in values)
    assert len(set(identities)) == len(values)
    resumed = tuple(ShardedSampleStream(values, start_index=5))
    assert [sample.sample_id for sample in resumed] == [f"s-{index}" for index in range(5, 11)]
    assert StreamCheckpoint(DIGEST, 5, 0, 7).next_index == 5


def test_bounded_shuffle_is_seeded_and_preserves_coverage() -> None:
    first = tuple(buffered_shuffle(range(20), buffer_size=4, seed=3))
    second = tuple(buffered_shuffle(range(20), buffer_size=4, seed=3))
    assert first == second
    assert sorted(first) == list(range(20))
