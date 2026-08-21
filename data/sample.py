# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit sample contract shared by model-neutral data loaders."""

from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Any

_IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,255}")
_SPLITS = {"train", "validation", "test", "holdout", "serving"}
MAX_FIELDS = 256


@dataclass(frozen=True, slots=True)
class Sample:
    """One prediction unit with an explicit leakage group and provenance digest."""

    sample_id: str
    features: Mapping[str, Any]
    provenance_digest: str
    group_id: str | None = None
    split: str | None = None
    label: Any | None = None
    metadata: Mapping[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        _identifier(self.sample_id, "sample_id")
        if self.group_id is not None:
            _identifier(self.group_id, "group_id")
        if self.split is not None and self.split not in _SPLITS:
            raise ValueError("sample split is invalid")
        if (
            not isinstance(self.provenance_digest, str)
            or len(self.provenance_digest) != 71
            or not self.provenance_digest.startswith("sha256:")
            or any(character not in "0123456789abcdef" for character in self.provenance_digest[7:])
        ):
            raise ValueError("sample provenance digest is invalid")
        if not isinstance(self.features, Mapping) or not 1 <= len(self.features) <= MAX_FIELDS:
            raise ValueError("sample features must contain 1..256 fields")
        if any(not isinstance(key, str) or not key for key in self.features):
            raise ValueError("sample feature names must be non-empty strings")
        if not isinstance(self.metadata, Mapping) or len(self.metadata) > MAX_FIELDS:
            raise ValueError("sample metadata exceeds bounds")
        if any(
            not isinstance(key, str)
            or not key
            or not isinstance(value, str)
            or not value
            or len(key) > 128
            or len(value) > 4096
            for key, value in self.metadata.items()
        ):
            raise ValueError("sample metadata is invalid")
        object.__setattr__(self, "features", MappingProxyType(dict(self.features)))
        object.__setattr__(self, "metadata", MappingProxyType(dict(self.metadata)))


def _identifier(value: object, name: str) -> str:
    if not isinstance(value, str) or not _IDENTIFIER.fullmatch(value):
        raise ValueError(f"{name} is invalid")
    return value
