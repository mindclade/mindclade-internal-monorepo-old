# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Digest-set contamination exclusion stage."""

from __future__ import annotations

from dataclasses import dataclass

from .fingerprints import payload_fingerprint
from .pipeline import CuratedRecord


@dataclass(frozen=True, slots=True)
class ExcludeFingerprints:
    forbidden: frozenset[str]

    def __call__(self, record: CuratedRecord) -> CuratedRecord | None:
        return None if payload_fingerprint(record) in self.forbidden else record
