# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import datetime as dt

from data.quality import FunctionValidator, QualityGate, group_split_findings
from data.sample import Sample

DIGEST = "sha256:" + "a" * 64


def sample(sample_id: str, group_id: str | None, split: str | None) -> Sample:
    return Sample(sample_id, {"value": 1}, DIGEST, group_id=group_id, split=split)


def test_quality_gate_blocks_group_leakage_and_emits_aggregate_only_evidence() -> None:
    gate = QualityGate(
        "ml-data-v1", (FunctionValidator("group-leakage", group_split_findings),)
    )
    report = gate.evaluate(
        DIGEST,
        (sample("s1", "donor-1", "train"), sample("s2", "donor-1", "test")),
        evaluated_at=dt.datetime(2026, 8, 20, tzinfo=dt.UTC),
    )
    assert not report.passed
    assert [(finding.code, finding.count) for finding in report.findings] == [
        ("group-split-leakage", 1)
    ]
    assert "donor-1" not in report.canonical_document()
    assert report.digest.startswith("sha256:")


def test_quality_gate_accepts_disjoint_complete_splits_deterministically() -> None:
    gate = QualityGate(
        "ml-data-v1", (FunctionValidator("group-leakage", group_split_findings),)
    )
    samples = (sample("s1", "donor-1", "train"), sample("s2", "donor-2", "test"))
    timestamp = dt.datetime(2026, 8, 20, tzinfo=dt.UTC)
    first = gate.evaluate(DIGEST, samples, evaluated_at=timestamp)
    second = gate.evaluate(DIGEST, reversed(samples), evaluated_at=timestamp)
    assert first.passed
    assert first.digest == second.digest
