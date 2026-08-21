# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Internal-source provenance stage with explicit approved-use evidence."""

from __future__ import annotations

from dataclasses import dataclass

from ..pipeline import CuratedRecord
from ..provenance import with_metadata


@dataclass(frozen=True, slots=True)
class InternalProvenance:
    snapshot_digest: str
    approved_uses: tuple[str, ...]

    def __post_init__(self) -> None:
        if not self.approved_uses or any("," in item or not item for item in self.approved_uses):
            raise ValueError("internal source approved uses are invalid")

    def __call__(self, record: CuratedRecord) -> CuratedRecord:
        value = with_metadata(record, "source", "internal")
        value = with_metadata(value, "source_snapshot_digest", self.snapshot_digest)
        return with_metadata(value, "approved_uses", ",".join(sorted(self.approved_uses)))
