# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Main-process host-to-device transfer; never executed in DataLoader workers."""

from __future__ import annotations

from collections.abc import Mapping
from types import MappingProxyType
from typing import Any

import torch

from .collate import CollatedBatch


def move_batch(
    batch: CollatedBatch, device: torch.device | str, *, non_blocking: bool = False
) -> CollatedBatch:
    resolved = torch.device(device)
    return CollatedBatch(
        batch.sample_ids,
        MappingProxyType(
            {key: _move(value, resolved, non_blocking) for key, value in batch.features.items()}
        ),
        batch.provenance_digests,
        batch.group_ids,
        batch.splits,
        _move(batch.labels, resolved, non_blocking),
    )


def _move(value: Any, device: torch.device, non_blocking: bool) -> Any:
    if isinstance(value, torch.Tensor):
        return value.to(device, non_blocking=non_blocking)
    if isinstance(value, Mapping):
        return {key: _move(item, device, non_blocking) for key, item in value.items()}
    if isinstance(value, tuple):
        return tuple(_move(item, device, non_blocking) for item in value)
    if isinstance(value, list):
        return [_move(item, device, non_blocking) for item in value]
    return value
