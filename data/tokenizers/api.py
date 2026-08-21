# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Tokenizer input/output contract."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from .vocabulary import Vocabulary


@dataclass(frozen=True, slots=True)
class EncodedSequence:
    token_ids: tuple[int, ...]
    vocabulary_digest: str
    tokenizer_version: str

    def __post_init__(self) -> None:
        if not self.token_ids or any(
            isinstance(token, bool) or not isinstance(token, int) or token < 0
            for token in self.token_ids
        ):
            raise ValueError("encoded token ids are invalid")
        if len(self.vocabulary_digest) != 71 or not self.vocabulary_digest.startswith("sha256:"):
            raise ValueError("encoded vocabulary digest is invalid")
        if not self.tokenizer_version or len(self.tokenizer_version) > 128:
            raise ValueError("encoded tokenizer version is invalid")


class Tokenizer(Protocol):
    @property
    def version(self) -> str: ...

    @property
    def vocabulary(self) -> Vocabulary: ...

    def encode(self, value: str) -> EncodedSequence: ...
