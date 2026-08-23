# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioural coverage for the preprocessing provenance records.

`Manifest.digest` is the identity a preprocessed input bundle is filed under: it is what a
later reader compares to decide whether two runs saw the same reference databases, the same
tools and the same configuration. That makes two things load-bearing and neither of them is
visible at a call site. First, the digest must move whenever anything it covers moves —
otherwise two materially different runs report as the same provenance and the record silently
stops being evidence. Second, the digest must NOT move for the same content — otherwise every
comparison across processes reports a difference that is not there, and the record becomes
noise nobody reads.

Tests labelled DOCUMENTED CURRENT BEHAVIOUR record what the shipped code does where that is
narrower than a reader would assume. They are not endorsements and they are not fixes: this
change adds coverage, and altering a digest's inputs is a change to published identities.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import FrozenInstanceError, replace

import pytest

from preprocessing.provenance import DatabaseSnapshot, Manifest, SearchRecord, ToolchainRecord


def _digest(marker: str) -> str:
    """A syntactically valid sha256 digest, distinct per single-character marker."""
    return "sha256:" + marker * 64


def _snapshot() -> DatabaseSnapshot:
    return DatabaseSnapshot(
        "refdb_golden",
        "uniref",
        "2026-01",
        _digest("1"),
        "2026-01-01",
        "mmseqs_index",
        "mmseqs",
        "2.1",
        (_digest("2"),),
    )


def _manifest() -> Manifest:
    """The golden manifest. Rebuilt per call so that "two equal manifests" means two objects."""
    return Manifest(
        1,
        "pipeline-golden/v1",
        _digest("9"),
        (_digest("3"),),
        (_snapshot(),),
        (
            SearchRecord(
                _digest("3"),
                _digest("1"),
                "mmseqs",
                "2.1",
                _digest("4"),
                _digest("5"),
                _digest("6"),
            ),
        ),
        (ToolchainRecord("mmseqs", "2.1", _digest("7"), _digest("8")),),
        _digest("a"),
    )


# --------------------------------------------------------------------------------------------
# database_snapshot.py
# --------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("release_id", ""),
        ("snapshot_digest", "deadbeef"),
        ("snapshot_digest", "sha512:" + "1" * 64),
        ("shard_digests", ()),
    ],
)
def test_database_snapshot_rejects_an_unverifiable_snapshot(field: str, value: object) -> None:
    # These three checks are the ones that make a snapshot re-fetchable and checkable later:
    # `release_id` names it, the "sha256:" prefix says which algorithm verifies it, and the
    # shard digests are what an auditor actually hashes. A snapshot missing any of them can
    # still be recorded in a manifest and quoted as provenance, which is worse than not having
    # recorded it — hence a constructor error rather than a validation pass somewhere later.
    with pytest.raises(ValueError, match="invalid database snapshot"):
        replace(_snapshot(), **{field: value})


def test_database_snapshot_validation_stops_at_three_fields() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR. `__post_init__` checks `release_id`, the digest prefix and
    # the shard list, and nothing else: empty name, empty version, empty source cutoff and
    # empty index-tool version all construct successfully. `source_cutoff` matters most here,
    # because it is the field a template-leakage argument rests on — "it constructed" is not
    # evidence that it was populated, and callers that need it validate it themselves.
    permissive = replace(
        _snapshot(), name="", version="", source_cutoff="", index_tool="", index_tool_version=""
    )

    assert permissive.source_cutoff == ""
    assert permissive.index_tool_version == ""
    assert permissive.snapshot_digest.startswith("sha256:")


# --------------------------------------------------------------------------------------------
# manifest.py — encoding
# --------------------------------------------------------------------------------------------


def test_manifest_digest_is_a_prefixed_lowercase_sha256_of_its_own_canonical_bytes() -> None:
    # The tie between the two is the whole audit story: an auditor holding `canonical_bytes()`
    # must be able to recompute the digest that was published, with no step in between that
    # only this process knows how to perform.
    manifest = _manifest()
    prefix, separator, hexdigest = manifest.digest.partition(":")

    assert prefix == "sha256"
    assert separator == ":"
    assert len(hexdigest) == 64
    assert set(hexdigest) <= set("0123456789abcdef")
    assert hexdigest == hashlib.sha256(manifest.canonical_bytes()).hexdigest()


def test_canonical_bytes_are_compact_and_key_sorted() -> None:
    # Sorted keys and separators without spaces are what make the encoding a function of the
    # content rather than of the field declaration order or of the json module's defaults. The
    # parsed key order is asserted rather than the raw byte layout because `json.loads` keeps
    # insertion order, so it reports exactly what the byte order was.
    raw = _manifest().canonical_bytes()
    decoded = json.loads(raw)

    assert list(decoded) == sorted(decoded)
    assert b", " not in raw
    assert b'": ' not in raw


def test_canonical_bytes_escape_non_ascii_rather_than_emitting_utf8() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR, and a divergence worth knowing about. This module hashes
    # `ensure_ascii=True` JSON, while `libs/python/serialization.canonical_json_bytes` — the
    # repository's cross-language canonical encoder — deliberately emits raw UTF-8 precisely so
    # that Go and Rust reproduce the same bytes. The two are therefore NOT interchangeable: a
    # reimplementation of this digest against the canonical serializer would disagree with
    # every manifest already recorded, and only for entries containing non-ASCII text.
    manifest = replace(_manifest(), pipeline_version="pipeline-café/v1")
    raw = manifest.canonical_bytes()

    assert max(raw) < 128
    assert b"caf\\u00e9" in raw
    assert json.loads(raw)["pipeline_version"] == "pipeline-café/v1"


def test_equal_manifests_produce_identical_bytes_and_digests() -> None:
    # Built twice from scratch, not aliased, because the property under test is that identity
    # comes from content: two processes assembling the same provenance must agree without
    # having shared an object.
    assert _manifest() == _manifest()
    assert _manifest().canonical_bytes() == _manifest().canonical_bytes()
    assert _manifest().digest == _manifest().digest


def test_manifest_digest_is_pinned_to_a_golden_value() -> None:
    # A published provenance identity. Changing the encoding does not fail anything by itself;
    # it re-identifies every manifest recorded up to that point, so that two runs that really
    # did see the same inputs no longer agree. A literal is the only assertion that notices,
    # since re-deriving the expectation would move with the code.
    assert (
        _manifest().digest
        == "sha256:94d40a28adb002d95bf72df59ca938082e62742779dfbb6b0bdbc4ab94754d69"
    )


# --------------------------------------------------------------------------------------------
# manifest.py — coverage of what the digest binds
# --------------------------------------------------------------------------------------------


def test_every_manifest_field_changes_the_digest() -> None:
    # One variant per declared field. A field that stopped reaching the digest would leave the
    # manifest agreeing with itself across exactly the change it was recorded to detect, and
    # nothing else in the system would notice.
    baseline = _manifest()
    variants = [
        baseline,
        replace(baseline, schema_version=2),
        replace(baseline, pipeline_version="pipeline-golden/v2"),
        replace(baseline, resolved_config_digest=_digest("b")),
        replace(baseline, entity_digests=(_digest("c"),)),
        replace(baseline, reference_databases=()),
        replace(baseline, searches=()),
        replace(baseline, tools=()),
        replace(baseline, output_artifact_digest=_digest("d")),
    ]
    digests = [manifest.digest for manifest in variants]

    assert len(set(digests)) == len(digests)


def test_manifest_digest_is_order_sensitive_where_a_cache_key_is_not() -> None:
    # The deliberate contrast with `preprocessing.cache.keys.cache_key`, which sorts its digests
    # so that irrelevant ordering does not fragment the cache. A manifest is the opposite kind
    # of object: it records a sequence as it was observed, and the order of the searches that
    # produced a bundle is part of what happened. Sorting here would erase that.
    second_snapshot = replace(_snapshot(), release_id="refdb_other", snapshot_digest=_digest("e"))
    baseline = replace(
        _manifest(),
        entity_digests=(_digest("3"), _digest("4")),
        reference_databases=(_snapshot(), second_snapshot),
    )
    reordered = replace(
        baseline,
        entity_digests=(_digest("4"), _digest("3")),
        reference_databases=(second_snapshot, _snapshot()),
    )

    assert baseline != reordered
    assert baseline.digest != reordered.digest


def test_nested_records_are_hashed_by_value() -> None:
    # `asdict` recurses, so a change buried inside a nested record has to surface in the
    # top-level digest. Without this the manifest would only be as sensitive as its own
    # outermost fields, and swapping a reference database for a different release of the same
    # name would be invisible.
    baseline = _manifest()
    changed_database = replace(
        baseline,
        reference_databases=(replace(_snapshot(), snapshot_digest=_digest("b")),),
    )

    assert baseline.digest != changed_database.digest


def test_raw_and_parsed_search_results_are_recorded_independently() -> None:
    # A search leaves two artifacts worth keeping: the bytes the tool emitted and the structure
    # the parser made of them. They move independently — a parser fix changes the parsed digest
    # with the raw output untouched — so a record that folded them together would let a reparse
    # pass as unchanged provenance.
    baseline = _manifest()
    search = baseline.searches[0]
    raw_changed = replace(baseline, searches=(replace(search, raw_result_digest=_digest("b")),))
    parsed_changed = replace(
        baseline, searches=(replace(search, parsed_result_digest=_digest("b")),)
    )

    assert len({baseline.digest, raw_changed.digest, parsed_changed.digest}) == 3


def test_toolchain_binary_and_arguments_are_recorded_independently() -> None:
    # The same declared version can be a different binary (a rebuild, a different build flag)
    # and the same binary can be a different computation (different arguments). Recording only
    # `version` would make two runs agree across a change that produced different numbers.
    baseline = _manifest()
    tool = baseline.tools[0]
    binary_changed = replace(baseline, tools=(replace(tool, binary_digest=_digest("b")),))
    arguments_changed = replace(baseline, tools=(replace(tool, arguments_digest=_digest("b")),))

    assert len({baseline.digest, binary_changed.digest, arguments_changed.digest}) == 3


def test_manifest_does_not_validate_what_it_hashes() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR, and an asymmetry inside this one package: `ArtifactRef` and
    # `DatabaseSnapshot` reject malformed digests in `__post_init__`, `Manifest` does not. A
    # manifest carrying a zero schema version and an output digest that is not a digest at all
    # constructs, and produces a perfectly well-formed sha256 over that nonsense. The manifest
    # is a recorder, not a gate — anything treating a computed `.digest` as evidence that the
    # contents were checked is reading it wrong.
    unvalidated = replace(_manifest(), schema_version=0, output_artifact_digest="not-a-digest")

    assert unvalidated.digest.startswith("sha256:")
    assert unvalidated.digest != _manifest().digest

    # Nor does it distinguish container types. `asdict` renders a tuple and a list identically,
    # so a manifest built with a list is an unequal object with an equal digest — the digest is
    # bound to content, not to the Python types the content arrived in.
    listed = replace(_manifest(), entity_digests=[_digest("3")])

    assert listed != _manifest()
    assert listed.digest == _manifest().digest


# --------------------------------------------------------------------------------------------
# immutability across all four records
# --------------------------------------------------------------------------------------------


def test_provenance_records_are_immutable() -> None:
    # Provenance is quoted after the fact, often from an object another component still holds a
    # reference to. A record that could be edited in place would let the evidence change after
    # the digest that certifies it was computed, which is the exact failure the digest exists
    # to make impossible.
    manifest = _manifest()

    with pytest.raises(FrozenInstanceError):
        manifest.pipeline_version = "rewritten"
    with pytest.raises(FrozenInstanceError):
        manifest.reference_databases[0].release_id = "rewritten"
    with pytest.raises(FrozenInstanceError):
        manifest.searches[0].tool = "rewritten"
    with pytest.raises(FrozenInstanceError):
        manifest.tools[0].version = "rewritten"
