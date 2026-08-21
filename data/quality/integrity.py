# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content and identity integrity validators."""

from __future__ import annotations

import hashlib
from collections.abc import Sequence

from data.sample import Sample

from .report import QualityFinding, Severity


def verify_bytes(payload: bytes, expected_digest: str, expected_size: int) -> None:
    if not isinstance(payload, bytes):
        raise TypeError("artifact payload must be bytes")
    actual = "sha256:" + hashlib.sha256(payload).hexdigest()
    if len(payload) != expected_size:
        raise ValueError("artifact size does not match manifest")
    if actual != expected_digest:
        raise ValueError("artifact digest does not match manifest")


def duplicate_sample_findings(samples: Sequence[Sample]) -> tuple[QualityFinding, ...]:
    counts: dict[str, int] = {}
    for sample in samples:
        counts[sample.sample_id] = counts.get(sample.sample_id, 0) + 1
    duplicates = sum(count - 1 for count in counts.values() if count > 1)
    if not duplicates:
        return ()
    return (
        QualityFinding(
            "duplicate-sample-id",
            Severity.BLOCKING,
            "dataset",
            duplicates,
            "sample identifiers must be unique within a dataset version",
        ),
    )
