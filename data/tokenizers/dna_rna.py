# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""IUPAC nucleotide tokenizer preserving DNA/RNA symbols."""

from __future__ import annotations

from dataclasses import dataclass, field

from .api import EncodedSequence
from .special_tokens import SpecialTokens
from .vocabulary import Vocabulary

NUCLEOTIDE_SYMBOLS = tuple("ACGTURYSWKMBDHVN-")


@dataclass(frozen=True, slots=True)
class NucleotideTokenizer:
    version: str = "iupac-nucleotide-v1"
    vocabulary: Vocabulary = field(
        default_factory=lambda: Vocabulary(SpecialTokens().ordered() + NUCLEOTIDE_SYMBOLS)
    )
    maximum_length: int = 10_000_000
    reject_unknown: bool = True

    def encode(self, value: str) -> EncodedSequence:
        if not isinstance(value, str) or not 1 <= len(value) <= self.maximum_length:
            raise ValueError("nucleotide sequence is outside bounds")
        tokens = (self.vocabulary.special.begin, *tuple(value.upper()), self.vocabulary.special.end)
        return EncodedSequence(
            self.vocabulary.encode(tokens, reject_unknown=self.reject_unknown),
            self.vocabulary.digest,
            self.version,
        )
