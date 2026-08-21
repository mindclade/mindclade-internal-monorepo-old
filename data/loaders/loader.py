# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Correct-by-construction PyTorch map-style loader assembly."""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass

import torch
from torch.utils.data import DataLoader, Dataset, DistributedSampler

from data.sample import Sample

from .collate import CollatedBatch, collate_samples
from .workers import seed_worker


class SampleDataset(Dataset[Sample]):
    """Stable finite dataset; samplers own ordering and sharding."""

    def __init__(self, samples: Sequence[Sample]) -> None:
        values = tuple(samples)
        if any(not isinstance(sample, Sample) for sample in values):
            raise TypeError("SampleDataset inputs must be Sample values")
        identities = [sample.sample_id for sample in values]
        if len(set(identities)) != len(identities):
            raise ValueError("SampleDataset sample identifiers must be unique")
        self._samples = values

    def __len__(self) -> int:
        return len(self._samples)

    def __getitem__(self, index: int) -> Sample:
        if isinstance(index, bool) or not isinstance(index, int):
            raise TypeError("dataset index must be an integer")
        if index < 0 or index >= len(self._samples):
            raise IndexError("dataset index is outside the addressable range")
        return self._samples[index]


@dataclass(frozen=True, slots=True)
class LoaderConfig:
    batch_size: int
    seed: int
    shuffle: bool = True
    drop_last: bool = False
    num_workers: int = 0
    pin_memory: bool = False
    persistent_workers: bool = False
    prefetch_factor: int | None = None

    def __post_init__(self) -> None:
        for value, name, maximum in (
            (self.batch_size, "batch_size", 65_536),
            (self.seed, "seed", 2**63 - 1),
            (self.num_workers, "num_workers", 1024),
        ):
            minimum = 1 if name == "batch_size" else 0
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not minimum <= value <= maximum
            ):
                raise ValueError(f"loader {name} is outside bounds")
        for value, name in (
            (self.shuffle, "shuffle"),
            (self.drop_last, "drop_last"),
            (self.pin_memory, "pin_memory"),
            (self.persistent_workers, "persistent_workers"),
        ):
            if not isinstance(value, bool):
                raise ValueError(f"loader {name} must be boolean")
        if self.persistent_workers and self.num_workers == 0:
            raise ValueError("persistent workers require num_workers > 0")
        if self.prefetch_factor is not None and (
            self.num_workers == 0
            or isinstance(self.prefetch_factor, bool)
            or not isinstance(self.prefetch_factor, int)
            or not 1 <= self.prefetch_factor <= 128
        ):
            raise ValueError("prefetch_factor requires workers and must be within bounds")


class EpochDataLoader:
    """Expose the required sampler ``set_epoch`` transition explicitly."""

    def __init__(
        self,
        loader: DataLoader[CollatedBatch],
        sampler: DistributedSampler[Sample] | None,
    ) -> None:
        self.loader = loader
        self._sampler = sampler

    def set_epoch(self, epoch: int) -> None:
        if isinstance(epoch, bool) or not isinstance(epoch, int) or epoch < 0:
            raise ValueError("loader epoch must be a non-negative integer")
        if self._sampler is not None:
            self._sampler.set_epoch(epoch)


def build_loader(
    dataset: SampleDataset,
    config: LoaderConfig,
    *,
    rank: int = 0,
    world_size: int = 1,
) -> EpochDataLoader:
    if not isinstance(dataset, SampleDataset) or not isinstance(config, LoaderConfig):
        raise TypeError("build_loader requires SampleDataset and LoaderConfig")
    if (
        isinstance(rank, bool)
        or not isinstance(rank, int)
        or isinstance(world_size, bool)
        or not isinstance(world_size, int)
        or world_size < 1
        or not 0 <= rank < world_size
    ):
        raise ValueError("loader rank/world_size is invalid")
    sampler: DistributedSampler[Sample] | None = None
    if world_size > 1:
        sampler = DistributedSampler(
            dataset,
            num_replicas=world_size,
            rank=rank,
            shuffle=config.shuffle,
            seed=config.seed,
            drop_last=config.drop_last,
        )
    generator = torch.Generator()
    generator.manual_seed(config.seed)
    kwargs: dict[str, object] = {}
    if config.prefetch_factor is not None:
        kwargs["prefetch_factor"] = config.prefetch_factor
    loader: DataLoader[CollatedBatch] = DataLoader(
        dataset,
        batch_size=config.batch_size,
        shuffle=config.shuffle if sampler is None else False,
        sampler=sampler,
        drop_last=config.drop_last,
        num_workers=config.num_workers,
        collate_fn=collate_samples,
        worker_init_fn=seed_worker,
        generator=generator,
        pin_memory=config.pin_memory,
        persistent_workers=config.persistent_workers,
        **kwargs,
    )
    return EpochDataLoader(loader, sampler)
