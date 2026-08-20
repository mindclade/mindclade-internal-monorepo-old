# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from typing import Any

import pytest

from libs.python.errors import Code, code_of
from libs.python.identifiers import ARTIFACT_REF_FIELDS, ArtifactRef, Digest

DIGEST_TEXT = "sha256:" + "a" * 64


def document(**overrides: Any) -> dict[str, Any]:
    base: dict[str, Any] = {
        "digest": DIGEST_TEXT,
        "size_bytes": 1234,
        "media_type": "application/vnd.mindclade.dataset-shard+bin",
        "logical_kind": "dataset-shard",
        "schema_version": 1,
    }
    base.update(overrides)
    return base


def test_field_set_is_exactly_the_cross_language_contract() -> None:
    # Spelled out rather than derived, so the assertion is against the contract
    # rather than against whatever the constant currently happens to say.
    expected = {"digest", "size_bytes", "media_type", "logical_kind", "schema_version"}
    assert expected == ARTIFACT_REF_FIELDS


def test_document_round_trips() -> None:
    assert ArtifactRef.from_document(document()).to_document() == document()


def test_schema_version_is_an_integer_and_orders_against_zero() -> None:
    # The defect this type resolves: worker_runtime typed this `str`, so the
    # cross-language assertion `schema_version > 0` raised TypeError.
    reference = ArtifactRef.from_document(document())
    assert isinstance(reference.schema_version, int)
    assert reference.schema_version > 0


@pytest.mark.parametrize("field", ["uri", "provider", "generation", "bucket"])
def test_location_fields_are_rejected(field: str) -> None:
    # ADR-0004 identifies an artifact by content. A reference carrying a location
    # would make the same bytes in two buckets two different artifacts.
    with pytest.raises(ValueError, match="fields must be exactly"):
        ArtifactRef.from_document(document(**{field: "x"}))


def test_missing_fields_are_rejected() -> None:
    incomplete = document()
    del incomplete["logical_kind"]
    with pytest.raises(ValueError, match="fields must be exactly"):
        ArtifactRef.from_document(incomplete)


def test_non_canonical_digest_is_rejected() -> None:
    with pytest.raises(ValueError) as caught:
        ArtifactRef.from_document(document(digest="sha256:" + "A" * 64))
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_negative_size_is_rejected_but_zero_is_allowed() -> None:
    with pytest.raises(ValueError, match="size"):
        ArtifactRef.from_document(document(size_bytes=-1))
    assert ArtifactRef.from_document(document(size_bytes=0)).size_bytes == 0


@pytest.mark.parametrize("media_type", ["", "application", "a" * 256 + "/b"])
def test_media_type_must_be_a_bounded_type_subtype(media_type: str) -> None:
    with pytest.raises(ValueError, match="media type"):
        ArtifactRef.from_document(document(media_type=media_type))


@pytest.mark.parametrize("logical_kind", ["", "k" * 129])
def test_logical_kind_is_required_and_bounded(logical_kind: str) -> None:
    with pytest.raises(ValueError, match="logical kind"):
        ArtifactRef.from_document(document(logical_kind=logical_kind))


@pytest.mark.parametrize("schema_version", [0, -1, True])
def test_schema_version_must_be_a_positive_non_boolean_integer(schema_version: object) -> None:
    # bool subclasses int, so True would otherwise pass as version 1.
    with pytest.raises(ValueError, match="schema version"):
        ArtifactRef.from_document(document(schema_version=schema_version))


def test_reference_is_hashable_and_immutable() -> None:
    reference = ArtifactRef.from_document(document())
    assert len({reference, ArtifactRef.from_document(document())}) == 1
    with pytest.raises(AttributeError):
        reference.size_bytes = 5  # type: ignore[misc]


def test_direct_construction_takes_a_digest_not_a_string() -> None:
    reference = ArtifactRef(Digest.of(b"payload"), 7, "application/octet-stream", "features", 1)
    assert reference.to_document()["digest"] == Digest.of(b"payload").text
