# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from data.curation.deduplication import deduplicate_payloads
from data.curation.filtering import RequireMetadata
from data.curation.pipeline import CuratedRecord, CurationPipeline, identity


def test_exact_payload_deduplication_is_order_independent_and_retains_stable_key() -> None:
    records = (
        CuratedRecord("z-key", b"same"),
        CuratedRecord("a-key", b"same"),
        CuratedRecord("b-key", b"different"),
    )
    assert [record.key for record in deduplicate_payloads(records)] == ["a-key", "b-key"]
    assert deduplicate_payloads(records) == deduplicate_payloads(reversed(records))


def test_explicit_metadata_filter_counts_drops_in_pipeline_evidence() -> None:
    stage = RequireMetadata("license_ref", frozenset({"approved"}))
    pipeline = CurationPipeline((stage, identity))
    result = pipeline.run(
        (
            CuratedRecord("a", b"accepted", (("license_ref", "approved"),)),
            CuratedRecord("b", b"dropped", (("license_ref", "denied"),)),
        )
    )
    assert [record.key for record in result.records] == ["a"]
    assert result.dropped_records == 1
