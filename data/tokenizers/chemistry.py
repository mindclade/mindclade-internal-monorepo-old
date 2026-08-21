# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded lexical tokenizer for already-canonicalized SMILES strings.

This performs lexical tokenization only; chemistry validation/canonicalization
belongs to the preprocessing chemistry package.
"""

from __future__ import annotations

import re

_SMILES_TOKEN = re.compile(
    r"\[[^\[\]]{1,128}\]|Br|Cl|Si|Se|Na|Li|Ca|Mg|Al|[A-Za-z]|%[0-9]{2}|[0-9]|[-=#$:.\\/()@+*]"
)


def lex_smiles(value: str, *, maximum_bytes: int = 1_000_000) -> tuple[str, ...]:
    if not isinstance(value, str) or not value or len(value.encode()) > maximum_bytes:
        raise ValueError("SMILES input is outside bounds")
    tokens = tuple(match.group(0) for match in _SMILES_TOKEN.finditer(value))
    if "".join(tokens) != value:
        raise ValueError("SMILES input contains an unsupported lexical token")
    return tokens
