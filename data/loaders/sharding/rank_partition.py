# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Disjoint rank partition for finite iterable sources."""

from __future__ import annotations


def rank_indices(length: int, rank: int, world_size: int) -> tuple[int, ...]:
    if any(isinstance(value, bool) or not isinstance(value, int) for value in (length, rank, world_size)):
        raise ValueError("rank partition values must be integers")
    if length < 0 or world_size < 1 or not 0 <= rank < world_size:
        raise ValueError("rank partition bounds are invalid")
    return tuple(range(rank, length, world_size))
