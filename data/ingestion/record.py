# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable canonical scientific record."""

from __future__ import annotations

import hashlib
from collections.abc import Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import Any

from .canonical import canonical_json


@dataclass(frozen=True, slots=True)
class CanonicalRecord:
    source_record_digest: str
    values: Mapping[str, Any]

    def __post_init__(self) -> None:
        if (
            not isinstance(self.source_record_digest, str)
            or len(self.source_record_digest) != 71
            or not self.source_record_digest.startswith("sha256:")
            or any(
                character not in "0123456789abcdef" for character in self.source_record_digest[7:]
            )
        ):
            raise ValueError("canonical record source digest is invalid")
        if not isinstance(self.values, Mapping) or not self.values or len(self.values) > 4096:
            raise ValueError("canonical record values are outside bounds")
        copied = dict(self.values)
        canonical_json(copied)
        object.__setattr__(self, "values", MappingProxyType(copied))

    @property
    def digest(self) -> str:
        return "sha256:" + hashlib.sha256(canonical_json(self.values)).hexdigest()
