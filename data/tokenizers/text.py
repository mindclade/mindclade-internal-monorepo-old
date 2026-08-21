# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Vocabulary-bound whitespace tokenizer for metadata text."""

from __future__ import annotations

from dataclasses import dataclass

from .api import EncodedSequence
from .vocabulary import Vocabulary


@dataclass(frozen=True, slots=True)
class TextTokenizer:
    vocabulary: Vocabulary
    version: str
    maximum_bytes: int = 1_000_000

    def encode(self, value: str) -> EncodedSequence:
        if not isinstance(value, str) or not value or len(value.encode()) > self.maximum_bytes:
            raise ValueError("text input is outside bounds")
        tokens = (
            self.vocabulary.special.begin,
            *tuple(value.split()),
            self.vocabulary.special.end,
        )
        return EncodedSequence(self.vocabulary.encode(tokens), self.vocabulary.digest, self.version)
