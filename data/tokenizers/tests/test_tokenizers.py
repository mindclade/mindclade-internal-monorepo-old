# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from data.tokenizers import NucleotideTokenizer, ProteinTokenizer, TokenizerRegistry
from data.tokenizers.chemistry import lex_smiles


def test_biological_tokenizers_are_versioned_and_deterministic() -> None:
    protein = ProteinTokenizer()
    assert protein.encode("ACDX") == protein.encode("acdx")
    nucleotide = NucleotideTokenizer()
    encoded = nucleotide.encode("ACGUN")
    assert encoded.vocabulary_digest == nucleotide.vocabulary.digest
    assert len(encoded.token_ids) == 7


def test_nucleotide_unknowns_fail_and_smiles_lexing_is_lossless() -> None:
    with pytest.raises(ValueError, match="outside"):
        NucleotideTokenizer().encode("ACG?")
    assert lex_smiles("CC(=O)Cl") == ("C", "C", "(", "=", "O", ")", "Cl")
    with pytest.raises(ValueError, match="unsupported"):
        lex_smiles("C C")


def test_registry_requires_exact_versions() -> None:
    protein = ProteinTokenizer()
    registry = TokenizerRegistry((("protein", protein),))
    assert registry.resolve("protein", protein.version) is protein
    with pytest.raises(KeyError, match="exact"):
        registry.resolve("protein", "latest")
