# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import datetime as dt
from pathlib import Path

import pytest

from data.manifest import ArtifactLocation, ArtifactRef
from data.reference import (
    ReferenceCatalog,
    ReferenceIndex,
    ReferenceSnapshot,
    ReferenceSource,
    parse_reference_snapshot,
    require_compatible_tool,
    validate_snapshot_locations,
)

DIGESTS = tuple("sha256:" + character * 64 for character in "abcd")


def snapshot() -> ReferenceSnapshot:
    artifact = ArtifactRef(DIGESTS[1], 100, "application/octet-stream", "reference-index", "v1")
    return ReferenceSnapshot(
        "uniref",
        "2026-08",
        (
            ReferenceSource(
                "uniprot",
                "2026_03",
                DIGESTS[0],
                "https://example.invalid/uniprot/2026_03.fasta.gz",
                dt.datetime(2026, 8, 1, tzinfo=dt.UTC),
                "CC-BY-4.0",
            ),
        ),
        (ReferenceIndex("msa", "v1", "mmseqs", "18.0", DIGESTS[2], (artifact,)),),
        ("mmseqs-18",),
        dt.datetime(2026, 8, 20, tzinfo=dt.UTC),
        DIGESTS[3],
    )


def test_reference_snapshot_is_reproducible_and_exactly_resolved() -> None:
    item = snapshot()
    assert item.digest == snapshot().digest
    assert ReferenceCatalog((item,)).resolve("uniref", "2026-08").digest == item.digest
    require_compatible_tool(item, "mmseqs-18")
    with pytest.raises(ValueError, match="incompatible"):
        require_compatible_tool(item, "mutable-latest")


def test_reference_locations_must_cover_and_bind_every_index_artifact() -> None:
    item = snapshot()
    digest = item.indexes[0].artifacts[0].digest
    validate_snapshot_locations(
        item,
        {
            digest: (
                ArtifactLocation(
                    digest,
                    "gcs",
                    "gs://mindclade-reference/uniref/index-0000",
                    "1700000000000000",
                ),
            )
        },
    )
    with pytest.raises(ValueError, match="coverage"):
        validate_snapshot_locations(item, {})


def test_reference_manifest_round_trips_and_rejects_unknown_fields() -> None:
    item = snapshot()
    hydrated = parse_reference_snapshot(item.canonical_document())
    assert hydrated == item
    assert hydrated.digest == item.digest

    unknown = item.canonical_document().replace(
        '"schema_version":1', '"extra":true,"schema_version":1'
    )
    with pytest.raises(ValueError, match="fields do not match"):
        parse_reference_snapshot(unknown)
    with pytest.raises(ValueError, match="duplicate field"):
        parse_reference_snapshot('{"schema_version":1,"schema_version":1}')


def test_checked_in_reference_examples_are_hydratable_non_releases() -> None:
    root = Path(__file__).resolve().parents[1] / "manifests"
    examples = [
        parse_reference_snapshot(path.read_text(encoding="utf-8"))
        for path in sorted(root.glob("*.json"))
    ]
    assert {item.reference_id for item in examples} == {"ccd", "pdb", "rnacentral", "uniref"}
    assert all(item.sources[0].license_ref == "EXAMPLE-NOT-FOR-RELEASE" for item in examples)
