# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Dataset-contract schema validator adapter."""

from __future__ import annotations

from collections.abc import Sequence

from data.contracts import DatasetContract, validate_record
from data.sample import Sample

from .report import QualityFinding, Severity


def schema_findings(
    samples: Sequence[Sample], contract: DatasetContract
) -> tuple[QualityFinding, ...]:
    counts: dict[str, int] = {}
    for sample in samples:
        for issue in validate_record(sample.features, contract):
            key = f"{issue.field}:{issue.code}"
            counts[key] = counts.get(key, 0) + 1
    return tuple(
        QualityFinding(
            "schema-violation",
            Severity.ERROR,
            key,
            count,
            "one or more sample feature records violate the dataset contract",
        )
        for key, count in sorted(counts.items())
    )
