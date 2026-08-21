# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""RNAcentral provenance stage configured from an approved source snapshot."""

from __future__ import annotations

from dataclasses import dataclass

from ..pipeline import CuratedRecord
from ..provenance import with_metadata


@dataclass(frozen=True, slots=True)
class RNAcentralProvenance:
    snapshot_digest: str
    license_ref: str

    def __call__(self, record: CuratedRecord) -> CuratedRecord:
        value = with_metadata(record, "source", "rnacentral")
        value = with_metadata(value, "source_snapshot_digest", self.snapshot_digest)
        return with_metadata(value, "license_ref", self.license_ref)
