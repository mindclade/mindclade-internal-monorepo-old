# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit fixed-width padding contract for integer token sequences."""

from __future__ import annotations

import torch


def pad_sequences(
    sequences: tuple[tuple[int, ...], ...], *, pad_token: int, width: int | None = None
) -> tuple[torch.Tensor, torch.Tensor]:
    if not sequences or any(not sequence for sequence in sequences):
        raise ValueError("sequence padding requires non-empty sequences")
    if any(
        isinstance(token, bool) or not isinstance(token, int)
        for sequence in sequences
        for token in sequence
    ):
        raise ValueError("sequence tokens must be integers")
    if isinstance(pad_token, bool) or not isinstance(pad_token, int):
        raise ValueError("padding token must be an integer")
    target = max(len(sequence) for sequence in sequences) if width is None else width
    if isinstance(target, bool) or not isinstance(target, int) or target < max(
        len(sequence) for sequence in sequences
    ):
        raise ValueError("padding width is too small")
    tokens = torch.full((len(sequences), target), pad_token, dtype=torch.int64)
    mask = torch.zeros((len(sequences), target), dtype=torch.bool)
    for row, sequence in enumerate(sequences):
        tokens[row, : len(sequence)] = torch.tensor(sequence, dtype=torch.int64)
        mask[row, : len(sequence)] = True
    return tokens, mask
