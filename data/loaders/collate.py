# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit PyTorch collation contract for validated samples."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from types import MappingProxyType
from typing import Any

import torch
from torch.utils.data import default_collate

from data.sample import Sample


@dataclass(frozen=True, slots=True)
class CollatedBatch:
    sample_ids: tuple[str, ...]
    features: Mapping[str, Any]
    provenance_digests: tuple[str, ...]
    group_ids: tuple[str | None, ...]
    splits: tuple[str | None, ...]
    labels: Any | None

    @property
    def size(self) -> int:
        return len(self.sample_ids)

    def pin_memory(self) -> CollatedBatch:
        """Hook used by PyTorch's pin-memory thread, never by loader workers."""

        return CollatedBatch(
            self.sample_ids,
            MappingProxyType({key: _pin(value) for key, value in self.features.items()}),
            self.provenance_digests,
            self.group_ids,
            self.splits,
            _pin(self.labels),
        )


def collate_samples(samples: Sequence[Sample]) -> CollatedBatch:
    items = tuple(samples)
    if not items or any(not isinstance(sample, Sample) for sample in items):
        raise ValueError("collation requires one or more Sample values")
    keys = tuple(sorted(items[0].features))
    if any(tuple(sorted(sample.features)) != keys for sample in items):
        raise ValueError("sample feature schemas differ within a batch")
    labels_present = [sample.label is not None for sample in items]
    if any(labels_present) and not all(labels_present):
        raise ValueError("labels must be present for every sample or none")
    try:
        features = {
            key: default_collate([sample.features[key] for sample in items]) for key in keys
        }
        labels = (
            default_collate([sample.label for sample in items]) if all(labels_present) else None
        )
    except (RuntimeError, TypeError, ValueError) as error:
        raise ValueError(
            "sample values do not satisfy the fixed-shape collation contract"
        ) from error
    _assert_host_values(features)
    _assert_host_values(labels)
    return CollatedBatch(
        tuple(sample.sample_id for sample in items),
        MappingProxyType(features),
        tuple(sample.provenance_digest for sample in items),
        tuple(sample.group_id for sample in items),
        tuple(sample.split for sample in items),
        labels,
    )


def _assert_host_values(value: object) -> None:
    if isinstance(value, torch.Tensor) and value.device.type != "cpu":
        raise ValueError("DataLoader workers may not return accelerator tensors")
    if isinstance(value, Mapping):
        for item in value.values():
            _assert_host_values(item)
    elif isinstance(value, Sequence) and not isinstance(value, str | bytes):
        for item in value:
            _assert_host_values(item)


def _pin(value: Any) -> Any:
    if isinstance(value, torch.Tensor):
        return value.pin_memory()
    if isinstance(value, Mapping):
        return {key: _pin(item) for key, item in value.items()}
    if isinstance(value, tuple):
        return tuple(_pin(item) for item in value)
    if isinstance(value, list):
        return [_pin(item) for item in value]
    return value
