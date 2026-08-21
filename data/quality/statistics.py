# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded aggregate sample statistics safe for quality evidence."""

from __future__ import annotations

from collections.abc import Sequence

from data.sample import Sample

from .report import QualityMetric


def sample_metrics(samples: Sequence[Sample]) -> tuple[QualityMetric, ...]:
    groups = {sample.group_id for sample in samples if sample.group_id is not None}
    labeled = sum(sample.label is not None for sample in samples)
    return (
        QualityMetric("sample-count", float(len(samples)), "records"),
        QualityMetric("group-count", float(len(groups)), "groups"),
        QualityMetric("labeled-sample-count", float(labeled), "records"),
    )
