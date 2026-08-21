# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Digest-level overlap audit between training and evaluation populations."""

from __future__ import annotations

from collections.abc import Iterable

from .report import QualityFinding, Severity


def evaluation_overlap_findings(
    training_digests: Iterable[str], evaluation_digests: Iterable[str]
) -> tuple[QualityFinding, ...]:
    training = set(training_digests)
    evaluation = set(evaluation_digests)
    overlap = len(training & evaluation)
    if not overlap:
        return ()
    return (
        QualityFinding(
            "training-evaluation-overlap",
            Severity.BLOCKING,
            "dataset-pair",
            overlap,
            "content identities overlap between training and evaluation datasets",
        ),
    )
