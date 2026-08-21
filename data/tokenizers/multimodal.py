# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Combine encoded modalities while retaining modality boundaries."""

from __future__ import annotations

from collections.abc import Mapping

from .api import EncodedSequence


def combine_modalities(
    modalities: Mapping[str, EncodedSequence], *, separator_token_id: int
) -> tuple[tuple[int, ...], tuple[str, ...]]:
    if not modalities or len(modalities) > 32:
        raise ValueError("multimodal encoding requires 1..32 modalities")
    if (
        isinstance(separator_token_id, bool)
        or not isinstance(separator_token_id, int)
        or separator_token_id < 0
    ):
        raise ValueError("multimodal separator id is invalid")
    tokens: list[int] = []
    owners: list[str] = []
    for index, (name, encoded) in enumerate(sorted(modalities.items())):
        if not name or not isinstance(encoded, EncodedSequence):
            raise ValueError("multimodal encoding entry is invalid")
        if index:
            tokens.append(separator_token_id)
            owners.append("separator")
        tokens.extend(encoded.token_ids)
        owners.extend([name] * len(encoded.token_ids))
    return tuple(tokens), tuple(owners)
