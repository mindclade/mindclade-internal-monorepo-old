# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate pre-quantized structure token streams without hidden numerics."""

from __future__ import annotations


def validate_structure_tokens(
    token_ids: tuple[int, ...], *, vocabulary_size: int, maximum_length: int = 1_000_000
) -> tuple[int, ...]:
    if (
        isinstance(vocabulary_size, bool)
        or not isinstance(vocabulary_size, int)
        or vocabulary_size < 1
        or not 1 <= len(token_ids) <= maximum_length
        or any(
            isinstance(token, bool)
            or not isinstance(token, int)
            or not 0 <= token < vocabulary_size
            for token in token_ids
        )
    ):
        raise ValueError("structure token stream is invalid")
    return tuple(token_ids)
