# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import datetime as dt

import pytest

from data.contracts import (
    DatasetContract,
    DatasetSnapshot,
    FieldContract,
    FieldType,
    LogPolicy,
    Sensitivity,
    ShardManifest,
    SourceSnapshot,
    validate_record,
)

DIGEST = "sha256:" + "a" * 64
UTC = dt.UTC


def source() -> SourceSnapshot:
    return SourceSnapshot(
        uri="gs://mindclade-internal-data/source/snapshot-1",
        digest=DIGEST,
        captured_at=dt.datetime(2026, 8, 20, tzinfo=UTC),
        owner="data-platform",
        classification=Sensitivity.PROPRIETARY_INTERNAL,
        license_ref="Mindclade-internal-use",
        use_constraints=("internal-training-only", "no-verbatim-telemetry"),
    )


def contract(**changes: object) -> DatasetContract:
    values: dict[str, object] = {
        "dataset_id": "frontier-training",
        "version": "1.0.0",
        "owner": "data-platform",
        "fields": (
            FieldContract("record_id", FieldType.STRING),
            FieldContract("event_time", FieldType.TIMESTAMP),
            FieldContract("ingestion_time", FieldType.TIMESTAMP),
            FieldContract(
                "score",
                FieldType.FLOAT,
                minimum=0.0,
                maximum=1.0,
                sensitivity=Sensitivity.PROPRIETARY_INTERNAL,
                log_policy=LogPolicy.DROP,
            ),
        ),
        "primary_keys": ("record_id",),
        "event_time_field": "event_time",
        "ingestion_time_field": "ingestion_time",
        "schema_digest": DIGEST,
        "sources": (source(),),
        "allowed_lateness_seconds": 60,
    }
    values.update(changes)
    return DatasetContract(**values)  # type: ignore[arg-type]


def test_contract_rejects_sensitive_log_exposure_and_unsigned_sources() -> None:
    with pytest.raises(ValueError, match="may not be emitted"):
        FieldContract(
            "sequence",
            FieldType.STRING,
            sensitivity=Sensitivity.RESTRICTED,
            log_policy=LogPolicy.ALLOW,
        )
    with pytest.raises(ValueError, match="unsigned"):
        SourceSnapshot(
            uri="gs://bucket/object?signature=secret",
            digest=DIGEST,
            captured_at=dt.datetime.now(UTC),
            owner="data-platform",
            classification=Sensitivity.PROPRIETARY_INTERNAL,
            license_ref="internal",
            use_constraints=("training",),
        )


def test_contract_rejects_undeclared_keys_and_time_fields() -> None:
    with pytest.raises(ValueError, match="undeclared"):
        contract(primary_keys=("missing",))
    with pytest.raises(ValueError, match="non-null timestamps"):
        contract(event_time_field="record_id")


def test_record_validation_is_non_coercing_and_stable() -> None:
    event = dt.datetime(2026, 8, 20, tzinfo=UTC)
    issues = validate_record(
        {
            "record_id": "r-1",
            "event_time": event,
            "ingestion_time": event + dt.timedelta(seconds=61),
            "score": 2.0,
            "undeclared": "rejected",
        },
        contract(),
    )
    assert [(issue.field, issue.code) for issue in issues] == [
        ("ingestion_time", "late"),
        ("score", "maximum"),
        ("undeclared", "unknown"),
    ]


def test_record_validation_rejects_integer_as_float() -> None:
    event = dt.datetime(2026, 8, 20, tzinfo=UTC)
    issues = validate_record(
        {
            "record_id": "r-1",
            "event_time": event,
            "ingestion_time": event,
            "score": 1,
        },
        contract(),
    )
    assert [(issue.field, issue.code) for issue in issues] == [("score", "type")]


def test_record_validation_rejects_non_string_keys_and_non_finite_values() -> None:
    event = dt.datetime(2026, 8, 20, tzinfo=UTC)
    issues = validate_record(
        {
            7: "undeclared",
            "record_id": "r-1",
            "event_time": event,
            "ingestion_time": event,
            "score": float("nan"),
        },
        contract(),
    )
    assert [(issue.field, issue.code) for issue in issues] == [
        ("7", "unknown"),
        ("score", "finite"),
    ]


def test_contract_rejects_classification_downgrades_and_string_enum_bypasses() -> None:
    with pytest.raises(ValueError, match="Sensitivity"):
        FieldContract("sequence", FieldType.STRING, sensitivity="restricted")  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="downgrade"):
        contract(classification=Sensitivity.INTERNAL)


def test_snapshot_binds_exact_shard_counts_and_digests() -> None:
    shard = ShardManifest(
        uri="gs://mindclade-internal-data/dataset/shard-0001",
        digest=DIGEST,
        record_count=3,
        size_bytes=128,
    )
    snapshot = DatasetSnapshot(
        dataset_id="frontier-training",
        contract_version="1.0.0",
        contract_digest=DIGEST,
        transform_digest="sha256:" + "b" * 64,
        generated_at=dt.datetime(2026, 8, 20, tzinfo=UTC),
        split="train",
        seed=7,
        shards=(shard,),
        record_count=3,
    )
    assert snapshot.record_count == sum(item.record_count for item in snapshot.shards)
    with pytest.raises(ValueError, match="shard total"):
        DatasetSnapshot(**{**snapshot.__dict__, "record_count": 4})
