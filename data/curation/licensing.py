# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Approved-license allow-list stage; legal policy remains external evidence."""

from __future__ import annotations

from dataclasses import dataclass

from .pipeline import CuratedRecord


@dataclass(frozen=True, slots=True)
class RequireApprovedLicense:
    approved: frozenset[str]

    def __post_init__(self) -> None:
        if not self.approved or any(not item for item in self.approved):
            raise ValueError("license stage requires an approved set")

    def __call__(self, record: CuratedRecord) -> CuratedRecord | None:
        return record if dict(record.metadata).get("license_ref") in self.approved else None
