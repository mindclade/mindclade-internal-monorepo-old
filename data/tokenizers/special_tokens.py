# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit reserved-token contract."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SpecialTokens:
    pad: str = "<pad>"
    unknown: str = "<unk>"
    begin: str = "<bos>"
    end: str = "<eos>"
    separator: str = "<sep>"
    mask: str = "<mask>"

    def __post_init__(self) -> None:
        values = (self.pad, self.unknown, self.begin, self.end, self.separator, self.mask)
        if len(set(values)) != len(values) or any(
            not value or len(value.encode()) > 64 for value in values
        ):
            raise ValueError("special tokens must be unique, non-empty, and bounded")

    def ordered(self) -> tuple[str, ...]:
        return (self.pad, self.unknown, self.begin, self.end, self.separator, self.mask)
