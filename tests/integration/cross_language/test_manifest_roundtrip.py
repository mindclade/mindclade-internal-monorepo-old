# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Artifact-reference round trip across the proto, Python, and Go projections.

Per ADR-0004 an artifact is identified by what it contains, never by where it
sits, so `mindclade.common.v1.ArtifactRef` carries exactly five fields and
location lives on `ArtifactLocation`. Three hand-written projections have to
agree on that: the proto, `libs/python/identifiers.ArtifactRef`, and Go's
`control/artifacts.Ref`.

The docstring on `libs/python/identifiers/artifact.py` says this module "asserts
both halves of that" and that "the round-trip test asserts schema_version > 0".
It did not. The previous version read the `artifact_ref` object out of the
shared primitives fixture and checked its key set and a digest regex directly —
it never constructed an `ArtifactRef`, never round-tripped anything, and never
compared the field set with the proto or with Go, so the claim in that docstring
was the only thing tying the three projections together. This module does the
round trip and the three-way comparison for real.

The Go half of the same fixture is exercised by `golden_test.go`
(`TestPrimitiveGoldenParsesThroughGoContracts`), which parses it through
`artifacts.Ref` and validates it.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest
from test_worker_protocol import PROTO_ROOT, proto_messages

from libs.python.identifiers import ArtifactRef
from libs.python.identifiers.artifact import (
    ARTIFACT_REF_FIELDS,
    MAXIMUM_ARTIFACT_SIZE,
    MAXIMUM_SCHEMA_VERSION,
)

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = Path(__file__).parent / "fixtures" / "primitives_v1.json"
COMMON_V1 = PROTO_ROOT / "common/v1"
GO_CATALOG = ROOT / "control/artifacts/catalog.go"

# Placement, not identity. The same bytes in two buckets are one artifact, so
# none of these may appear on the reference in any language.
LOCATION_FIELDS = {"uri", "provider", "generation", "region"}


@pytest.fixture(scope="module")
def document() -> dict[str, object]:
    return json.loads(FIXTURE.read_text())["artifact_ref"]


def go_struct_json_tags(source: str, name: str) -> list[str]:
    """The `json:"..."` tags of one Go struct, in declaration order."""
    body = source.split(f"type {name} struct {{", 1)[1].split("\n}", 1)[0]
    return re.findall(r'json:"(\w+)"', body)


def test_python_round_trip_reproduces_the_shared_fixture(document):
    """Decode, re-encode, and land on exactly the bytes the fixture carries."""
    reference = ArtifactRef.from_document(document)
    assert reference.to_document() == document
    assert reference.digest.text == document["digest"]
    assert reference.size_bytes == document["size_bytes"]
    assert reference.schema_version > 0


def test_python_go_and_proto_agree_on_the_reference_field_set(document):
    """One identity, three hand-written projections, one field set."""
    proto_fields = list(proto_messages(COMMON_V1)["ArtifactRef"])
    go_fields = go_struct_json_tags(GO_CATALOG.read_text(), "Ref")
    assert set(document) == ARTIFACT_REF_FIELDS
    assert set(proto_fields) == ARTIFACT_REF_FIELDS, (
        f"proto ArtifactRef and Python disagree: {sorted(set(proto_fields) ^ ARTIFACT_REF_FIELDS)}"
    )
    assert set(go_fields) == ARTIFACT_REF_FIELDS, (
        f"Go artifacts.Ref and Python disagree: {sorted(set(go_fields) ^ ARTIFACT_REF_FIELDS)}"
    )
    # Field order is the declaration order in all three; keeping it aligned is
    # what makes a side-by-side review of the three files meaningful.
    assert go_fields == proto_fields


def test_reference_integer_widths_match_the_wire_types():
    """`size_bytes` is uint64 and `schema_version` is uint32 on the wire.

    Python has no fixed-width integers, so it declares the wire widths as
    explicit ceilings instead. This compares the three declarations; that the
    Python ceilings are actually enforced is unit-tested next to the type in
    `libs/python/identifiers/tests/test_artifact.py`.
    """
    proto_fields = proto_messages(COMMON_V1)["ArtifactRef"]
    assert proto_fields["size_bytes"]["type"] == "uint64"
    assert proto_fields["schema_version"]["type"] == "uint32"
    assert MAXIMUM_ARTIFACT_SIZE == (1 << 64) - 1
    assert MAXIMUM_SCHEMA_VERSION == (1 << 32) - 1
    go_source = GO_CATALOG.read_text()
    assert re.search(r"SizeBytes\s+uint64", go_source), "Go SizeBytes is no longer uint64"
    assert re.search(r"SchemaVersion\s+uint32", go_source), "Go SchemaVersion is no longer uint32"


def test_identity_never_carries_a_location():
    """ADR-0004: placement lives on ArtifactLocation, never on the reference."""
    messages = proto_messages(COMMON_V1)
    assert not LOCATION_FIELDS & set(messages["ArtifactRef"])
    assert set(messages["ArtifactLocation"]) > LOCATION_FIELDS, (
        "ArtifactLocation no longer carries the placement fields"
    )
    assert not LOCATION_FIELDS & ARTIFACT_REF_FIELDS
    go_source = GO_CATALOG.read_text()
    assert not LOCATION_FIELDS & set(go_struct_json_tags(go_source, "Ref"))


def test_fixture_digest_is_the_canonical_cross_language_form(document):
    """Every language parses this exact digest text; pin its shape here.

    Rejection of a malformed digest, a missing field, a location key, and a zero
    schema version is single-language validation and is covered where CLAUDE.md
    places it — `libs/python/identifiers/tests/test_artifact.py`. This module
    holds only what needs a second language to be meaningful.
    """
    assert re.fullmatch(r"sha256:[0-9a-f]{64}", str(document["digest"]))
