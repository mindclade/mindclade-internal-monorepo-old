# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable content-addressed token vocabulary."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field

from .special_tokens import SpecialTokens


@dataclass(frozen=True, slots=True)
class Vocabulary:
    tokens: tuple[str, ...]
    special: SpecialTokens = field(default_factory=SpecialTokens)

    def __post_init__(self) -> None:
        tokens = tuple(self.tokens)
        if not 1 <= len(tokens) <= 10_000_000 or any(
            not isinstance(token, str)
            or not token
            or len(token.encode("utf-8")) > 1024
            or any(ord(character) < 0x20 for character in token)
            for token in tokens
        ):
            raise ValueError("vocabulary tokens are outside bounds")
        if len(set(tokens)) != len(tokens):
            raise ValueError("vocabulary tokens must be unique")
        missing = set(self.special.ordered()) - set(tokens)
        if missing:
            raise ValueError("vocabulary is missing required special tokens")
        object.__setattr__(self, "tokens", tokens)

    def token_id(self, token: str, *, reject_unknown: bool = False) -> int:
        try:
            return self.tokens.index(token)
        except ValueError:
            if reject_unknown:
                raise ValueError(f"token is outside the vocabulary: {token!r}") from None
            return self.tokens.index(self.special.unknown)

    def encode(self, tokens: tuple[str, ...], *, reject_unknown: bool = False) -> tuple[int, ...]:
        index = {token: token_id for token_id, token in enumerate(self.tokens)}
        unknown = index[self.special.unknown]
        values: list[int] = []
        for token in tokens:
            if token not in index and reject_unknown:
                raise ValueError(f"token is outside the vocabulary: {token!r}")
            values.append(index.get(token, unknown))
        return tuple(values)

    @property
    def digest(self) -> str:
        canonical = json.dumps(list(self.tokens), ensure_ascii=True, separators=(",", ":")).encode()
        return "sha256:" + hashlib.sha256(canonical).hexdigest()
