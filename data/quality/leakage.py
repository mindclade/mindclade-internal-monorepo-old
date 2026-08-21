# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Leakage audit across subject/donor/family/replicate grouping identities."""

from __future__ import annotations

from collections.abc import Sequence

from data.sample import Sample

from .report import QualityFinding, Severity


def group_split_findings(samples: Sequence[Sample]) -> tuple[QualityFinding, ...]:
    memberships: dict[str, set[str]] = {}
    missing_groups = 0
    missing_splits = 0
    for sample in samples:
        if sample.group_id is None:
            missing_groups += 1
            continue
        if sample.split is None:
            missing_splits += 1
            continue
        memberships.setdefault(sample.group_id, set()).add(sample.split)
    leaking = sum(1 for splits in memberships.values() if len(splits) > 1)
    findings: list[QualityFinding] = []
    if missing_groups:
        findings.append(
            QualityFinding(
                "missing-leakage-group",
                Severity.BLOCKING,
                "dataset",
                missing_groups,
                "every ML sample must carry its policy-defined grouping identity",
            )
        )
    if missing_splits:
        findings.append(
            QualityFinding(
                "missing-split",
                Severity.BLOCKING,
                "dataset",
                missing_splits,
                "every grouped ML sample must belong to a frozen lifecycle split",
            )
        )
    if leaking:
        findings.append(
            QualityFinding(
                "group-split-leakage",
                Severity.BLOCKING,
                "dataset",
                leaking,
                "a grouping identity occurs in more than one lifecycle split",
            )
        )
    return tuple(findings)
