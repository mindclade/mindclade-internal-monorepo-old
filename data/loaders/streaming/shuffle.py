# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded deterministic shuffle buffer for iterable sources."""

from __future__ import annotations

import random
from collections.abc import Iterable, Iterator
from typing import TypeVar

T = TypeVar("T")


def buffered_shuffle(values: Iterable[T], *, buffer_size: int, seed: int) -> Iterator[T]:
    if (
        isinstance(buffer_size, bool)
        or not isinstance(buffer_size, int)
        or buffer_size < 1
        or isinstance(seed, bool)
        or not isinstance(seed, int)
        or seed < 0
    ):
        raise ValueError("shuffle buffer configuration is invalid")
    source = iter(values)
    buffer: list[T] = []
    for _ in range(buffer_size):
        try:
            buffer.append(next(source))
        except StopIteration:
            break
    rng = random.Random(seed)
    while buffer:
        index = rng.randrange(len(buffer))
        yield buffer[index]
        try:
            buffer[index] = next(source)
        except StopIteration:
            buffer.pop(index)
