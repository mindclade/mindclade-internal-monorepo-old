# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Residue-level protein tokenizer with explicit ambiguous symbols."""

from __future__ import annotations

from dataclasses import dataclass, field

from .api import EncodedSequence
from .special_tokens import SpecialTokens
from .vocabulary import Vocabulary

PROTEIN_SYMBOLS = tuple("ACDEFGHIKLMNPQRSTVWYBXZJUO*")


@dataclass(frozen=True, slots=True)
class ProteinTokenizer:
    version: str = "protein-residue-v1"
    vocabulary: Vocabulary = field(
        default_factory=lambda: Vocabulary(SpecialTokens().ordered() + PROTEIN_SYMBOLS)
    )
    maximum_length: int = 1_000_000

    def encode(self, value: str) -> EncodedSequence:
        if not isinstance(value, str) or not 1 <= len(value) <= self.maximum_length:
            raise ValueError("protein sequence is outside bounds")
        tokens = (self.vocabulary.special.begin, *tuple(value.upper()), self.vocabulary.special.end)
        return EncodedSequence(self.vocabulary.encode(tokens), self.vocabulary.digest, self.version)
