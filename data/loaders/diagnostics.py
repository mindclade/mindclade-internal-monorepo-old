# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Coverage and throughput diagnostics independent of model computation."""

from __future__ import annotations

import time
from collections.abc import Iterable
from dataclasses import dataclass

from .collate import CollatedBatch


@dataclass(frozen=True, slots=True)
class CoverageReport:
    expected: int
    observed: int
    duplicates: int
    missing: int

    @property
    def complete(self) -> bool:
        return self.duplicates == 0 and self.missing == 0 and self.expected == self.observed


def audit_coverage(expected_ids: Iterable[str], batches: Iterable[CollatedBatch]) -> CoverageReport:
    expected = tuple(expected_ids)
    observed = [sample_id for batch in batches for sample_id in batch.sample_ids]
    if len(set(expected)) != len(expected):
        raise ValueError("expected coverage identities must be unique")
    counts: dict[str, int] = {}
    for sample_id in observed:
        counts[sample_id] = counts.get(sample_id, 0) + 1
    duplicates = sum(count - 1 for count in counts.values() if count > 1)
    missing = len(set(expected) - set(observed))
    unexpected = len(set(observed) - set(expected))
    return CoverageReport(len(expected), len(observed), duplicates + unexpected, missing)


def measure_batches(batches: Iterable[CollatedBatch]) -> tuple[int, float]:
    started = time.perf_counter()
    samples = sum(batch.size for batch in batches)
    elapsed = time.perf_counter() - started
    return samples, elapsed
