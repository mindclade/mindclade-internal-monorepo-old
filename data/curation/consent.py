# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Consent/data-use allow-list stage for already-pseudonymized records."""

from __future__ import annotations

from dataclasses import dataclass

from .pipeline import CuratedRecord


@dataclass(frozen=True, slots=True)
class RequireDataUse:
    required_use: str

    def __post_init__(self) -> None:
        if not self.required_use or len(self.required_use) > 256:
            raise ValueError("required data use is invalid")

    def __call__(self, record: CuratedRecord) -> CuratedRecord | None:
        allowed = set(dict(record.metadata).get("approved_uses", "").split(","))
        return record if self.required_use in allowed else None
