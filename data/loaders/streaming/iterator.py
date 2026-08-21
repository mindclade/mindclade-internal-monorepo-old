# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Finite iterable dataset sharded jointly by distributed rank and worker."""

from __future__ import annotations

from collections.abc import Iterator, Sequence

from torch.utils.data import IterableDataset, get_worker_info

from data.loaders.sharding import worker_indices
from data.sample import Sample


class ShardedSampleStream(IterableDataset[Sample]):
    def __init__(
        self,
        samples: Sequence[Sample],
        *,
        rank: int = 0,
        world_size: int = 1,
        start_index: int = 0,
    ) -> None:
        values = tuple(samples)
        if any(not isinstance(sample, Sample) for sample in values):
            raise TypeError("stream values must be Sample instances")
        if len({sample.sample_id for sample in values}) != len(values):
            raise ValueError("stream sample identities must be unique")
        if (
            isinstance(start_index, bool)
            or not isinstance(start_index, int)
            or not 0 <= start_index <= len(values)
        ):
            raise ValueError("stream start index is invalid")
        if world_size < 1 or not 0 <= rank < world_size:
            raise ValueError("stream rank/world_size is invalid")
        self._samples = values
        self._rank = rank
        self._world_size = world_size
        self._start_index = start_index

    def __iter__(self) -> Iterator[Sample]:
        worker = get_worker_info()
        worker_id = 0 if worker is None else worker.id
        num_workers = 1 if worker is None else worker.num_workers
        for index in worker_indices(
            len(self._samples),
            rank=self._rank,
            world_size=self._world_size,
            worker_id=worker_id,
            num_workers=num_workers,
        ):
            if index >= self._start_index:
                yield self._samples[index]
