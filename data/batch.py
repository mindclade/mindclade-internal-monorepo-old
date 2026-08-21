# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated batch envelope that preserves exact denominator semantics."""

from __future__ import annotations

from dataclasses import dataclass

from .sample import Sample

MAX_BATCH_SIZE = 65_536


@dataclass(frozen=True, slots=True)
class Batch:
    samples: tuple[Sample, ...]
    epoch: int
    batch_index: int
    is_partial: bool = False

    def __post_init__(self) -> None:
        samples = tuple(self.samples)
        if not 1 <= len(samples) <= MAX_BATCH_SIZE or any(
            not isinstance(sample, Sample) for sample in samples
        ):
            raise ValueError("batch requires 1..65536 samples")
        identities = [sample.sample_id for sample in samples]
        if len(set(identities)) != len(identities):
            raise ValueError("batch sample identities must be unique")
        for value, name in ((self.epoch, "epoch"), (self.batch_index, "batch_index")):
            if isinstance(value, bool) or not isinstance(value, int) or value < 0:
                raise ValueError(f"batch {name} must be a non-negative integer")
        if not isinstance(self.is_partial, bool):
            raise ValueError("batch is_partial must be boolean")
        object.__setattr__(self, "samples", samples)

    @property
    def size(self) -> int:
        return len(self.samples)
