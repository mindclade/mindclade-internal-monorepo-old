# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib

import pytest

from data.quality import duplicate_sample_findings, verify_bytes
from data.quality.evaluation_leakage import evaluation_overlap_findings
from data.sample import Sample

DIGEST = "sha256:" + "a" * 64


def test_byte_integrity_checks_size_and_digest() -> None:
    payload = b"immutable artifact"
    digest = "sha256:" + hashlib.sha256(payload).hexdigest()
    verify_bytes(payload, digest, len(payload))
    with pytest.raises(ValueError, match="size"):
        verify_bytes(payload, digest, len(payload) + 1)
    with pytest.raises(ValueError, match="digest"):
        verify_bytes(payload, DIGEST, len(payload))


def test_duplicate_and_evaluation_overlap_are_blocking() -> None:
    sample = Sample("sample-1", {"value": 1}, DIGEST, group_id="group-1", split="train")
    findings = duplicate_sample_findings((sample, sample))
    assert findings[0].code == "duplicate-sample-id"
    assert findings[0].count == 1
    overlap = evaluation_overlap_findings((DIGEST,), (DIGEST,))
    assert overlap[0].code == "training-evaluation-overlap"
