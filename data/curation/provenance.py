# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable provenance metadata updates."""

from __future__ import annotations

from .pipeline import CuratedRecord


def with_metadata(record: CuratedRecord, key: str, value: str) -> CuratedRecord:
    if not key or not value:
        raise ValueError("provenance key/value must be non-empty")
    metadata = dict(record.metadata)
    previous = metadata.get(key)
    if previous is not None and previous != value:
        raise ValueError("provenance metadata is immutable once recorded")
    metadata[key] = value
    result = CuratedRecord(record.key, record.payload, tuple(sorted(metadata.items())))
    result.validate()
    return result
