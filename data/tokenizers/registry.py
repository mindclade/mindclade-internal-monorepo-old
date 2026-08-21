# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact-version tokenizer registry."""

from __future__ import annotations

from collections.abc import Iterable

from .api import Tokenizer


class TokenizerRegistry:
    def __init__(self, tokenizers: Iterable[tuple[str, Tokenizer]]) -> None:
        entries: dict[tuple[str, str], Tokenizer] = {}
        for name, tokenizer in tokenizers:
            if not name or not hasattr(tokenizer, "encode"):
                raise ValueError("tokenizer registry entry is invalid")
            key = (name, tokenizer.version)
            if key in entries:
                raise ValueError("tokenizer registry identity/version must be unique")
            entries[key] = tokenizer
        self._entries = entries

    def resolve(self, name: str, version: str) -> Tokenizer:
        try:
            return self._entries[(name, version)]
        except KeyError as error:
            raise KeyError(f"unknown exact tokenizer {name}@{version}") from error
