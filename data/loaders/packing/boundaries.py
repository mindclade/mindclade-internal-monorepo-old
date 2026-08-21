# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Length-bucket boundary selection."""

from __future__ import annotations


def bucket_for(length: int, boundaries: tuple[int, ...]) -> int:
    if (
        isinstance(length, bool)
        or not isinstance(length, int)
        or length < 0
        or not boundaries
        or any(
            isinstance(value, bool) or not isinstance(value, int) or value < 1
            for value in boundaries
        )
        or tuple(sorted(set(boundaries))) != boundaries
    ):
        raise ValueError("packing bucket inputs are invalid")
    for boundary in boundaries:
        if length <= boundary:
            return boundary
    raise ValueError("sequence length exceeds the largest packing boundary")
