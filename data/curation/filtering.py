# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit metadata allow-list filter stage."""

from __future__ import annotations

from dataclasses import dataclass

from .pipeline import CuratedRecord


@dataclass(frozen=True, slots=True)
class RequireMetadata:
    key: str
    allowed_values: frozenset[str]

    def __post_init__(self) -> None:
        if (
            not self.key
            or not self.allowed_values
            or any(not value for value in self.allowed_values)
        ):
            raise ValueError("metadata filter requires a key and allowed values")

    def __call__(self, record: CuratedRecord) -> CuratedRecord | None:
        return record if dict(record.metadata).get(self.key) in self.allowed_values else None
