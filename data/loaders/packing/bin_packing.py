# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic first-fit-decreasing length packing."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class PackedBin:
    indices: tuple[int, ...]
    used: int
    capacity: int


def pack_lengths(lengths: tuple[int, ...], capacity: int) -> tuple[PackedBin, ...]:
    if (
        isinstance(capacity, bool)
        or not isinstance(capacity, int)
        or capacity < 1
        or any(
            isinstance(length, bool) or not isinstance(length, int) or not 1 <= length <= capacity
            for length in lengths
        )
    ):
        raise ValueError("packing lengths/capacity are invalid")
    bins: list[list[int]] = []
    used: list[int] = []
    for index in sorted(range(len(lengths)), key=lambda item: (-lengths[item], item)):
        for bin_index, occupied in enumerate(used):
            if occupied + lengths[index] <= capacity:
                bins[bin_index].append(index)
                used[bin_index] += lengths[index]
                break
        else:
            bins.append([index])
            used.append(lengths[index])
    return tuple(
        PackedBin(tuple(sorted(indices)), occupied, capacity)
        for indices, occupied in zip(bins, used, strict=True)
    )
