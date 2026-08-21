# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import datetime as dt
import json

import pytest

from data.contracts import (
    DatasetContract,
    FieldContract,
    FieldType,
    Sensitivity,
    SourceSnapshot,
)
from data.ingestion import IngestionPipeline, RawRecord, StageKind, StageSpec

DIGEST = "sha256:" + "a" * 64
SCHEMA_DIGEST = "sha256:" + "b" * 64
UTC = dt.UTC


def contract() -> DatasetContract:
    return DatasetContract(
        dataset_id="sequence-records",
        version="1.0.0",
        owner="data-platform",
        fields=(
            FieldContract("record_id", FieldType.STRING),
            FieldContract("sequence", FieldType.STRING),
            FieldContract("event_time", FieldType.TIMESTAMP),
            FieldContract("ingestion_time", FieldType.TIMESTAMP),
        ),
        primary_keys=("record_id",),
        event_time_field="event_time",
        ingestion_time_field="ingestion_time",
        schema_digest=SCHEMA_DIGEST,
        sources=(
            SourceSnapshot(
                uri="https://example.invalid/snapshots/sequences.json",
                digest=DIGEST,
                captured_at=dt.datetime(2026, 8, 20, tzinfo=UTC),
                owner="data-platform",
                classification=Sensitivity.PUBLIC,
                license_ref="CC0-1.0",
                use_constraints=("research",),
            ),
        ),
        classification=Sensitivity.INTERNAL,
        allowed_lateness_seconds=60,
    )


def stage() -> StageSpec:
    return StageSpec(StageKind.CANONICALIZE, "parser-1.0.0", DIGEST, SCHEMA_DIGEST, True)


def raw(key: str, record_id: str, *, sequence: str = "ACGU") -> RawRecord:
    value = {"record_id": record_id, "sequence": sequence}
    return RawRecord(key, json.dumps(value).encode(), DIGEST, int(key.removeprefix("r")))


def parse(record: RawRecord) -> dict[str, object]:
    value = json.loads(record.payload)
    event = dt.datetime(2026, 8, 20, tzinfo=UTC)
    return {**value, "event_time": event, "ingestion_time": event}


def test_pipeline_is_order_independent_and_manifest_is_reproducible() -> None:
    pipeline = IngestionPipeline(contract(), stage(), parse)
    first = pipeline.run((raw("r2", "id-2"), raw("r1", "id-1")))
    second = pipeline.run((raw("r1", "id-1"), raw("r2", "id-2")))
    assert [record.values["record_id"] for record in first.records] == ["id-1", "id-2"]
    assert first.manifest_digest == second.manifest_digest
    assert first.input_records == 2


def test_pipeline_coalesces_exact_duplicates_and_rejects_invalid_records() -> None:
    pipeline = IngestionPipeline(contract(), stage(), parse)
    result = pipeline.run((raw("r1", "id-1"), raw("r1", "id-1"), raw("r2", "id-2", sequence="")))
    assert len(result.records) == 2
    assert result.duplicate_records == 1
    assert len(result.rejected) == 0

    invalid = RawRecord("r3", b'{"record_id":"id-3"}', DIGEST, 3)
    rejected = pipeline.run((invalid,))
    assert [(issue.field, issue.code) for issue in rejected.rejected[0].issues] == [
        ("sequence", "missing")
    ]


def test_pipeline_rejects_conflicting_primary_keys_and_schema_mismatch() -> None:
    pipeline = IngestionPipeline(contract(), stage(), parse)
    with pytest.raises(ValueError, match="conflicting"):
        pipeline.run((raw("r1", "id-1", sequence="AAAA"), raw("r2", "id-1", sequence="CCCC")))
    with pytest.raises(ValueError, match="schema"):
        IngestionPipeline(
            contract(),
            StageSpec(StageKind.CANONICALIZE, "parser-1", DIGEST, DIGEST, True),
            parse,
        )
