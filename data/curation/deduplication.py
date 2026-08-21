# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic exact-payload deduplication."""

from __future__ import annotations

from collections.abc import Iterable

from .fingerprints import payload_fingerprint
from .pipeline import CuratedRecord


def deduplicate_payloads(records: Iterable[CuratedRecord]) -> tuple[CuratedRecord, ...]:
    by_fingerprint: dict[str, CuratedRecord] = {}
    for record in records:
        fingerprint = payload_fingerprint(record)
        previous = by_fingerprint.get(fingerprint)
        if previous is None or record.key < previous.key:
            by_fingerprint[fingerprint] = record
    return tuple(sorted(by_fingerprint.values(), key=lambda record: record.key))
