# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate aligned multimodal token blocks without implicit truncation."""

from __future__ import annotations

from collections.abc import Mapping


def validate_modalities(values: Mapping[str, tuple[int, ...]], *, maximum_tokens: int) -> None:
    if not values or len(values) > 32:
        raise ValueError("multimodal packing requires 1..32 modalities")
    if isinstance(maximum_tokens, bool) or not isinstance(maximum_tokens, int) or maximum_tokens < 1:
        raise ValueError("multimodal token limit is invalid")
    if any(
        not name
        or not tokens
        or any(isinstance(token, bool) or not isinstance(token, int) for token in tokens)
        for name, tokens in values.items()
    ):
        raise ValueError("multimodal token values are invalid")
    if sum(len(tokens) for tokens in values.values()) > maximum_tokens:
        raise ValueError("multimodal sample exceeds the token budget")
