# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Summarize bounded movement caused by a worker-set change."""

from __future__ import annotations

from dataclasses import dataclass

from .assignment import assign


@dataclass(frozen=True, slots=True)
class RebalancePlan:
    assignments: dict[str, str]
    moved_shards: tuple[str, ...]


def rebalance(
    shard_ids: tuple[str, ...], previous: dict[str, str], workers: tuple[str, ...]
) -> RebalancePlan:
    current = assign(shard_ids, workers)
    moved = tuple(
        sorted(shard for shard, worker in current.items() if previous.get(shard) != worker)
    )
    return RebalancePlan(current, moved)
