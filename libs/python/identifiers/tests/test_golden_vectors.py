# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bind this package to the shared cross-language vectors.

`tests/integration/cross_language/` asserts the fixture is *well formed* — that
the digest matches a pattern, that the UUID bits are right. Those tests pass
whether or not any Python implementation exists, which is how Python came to have
no seat at a table Go and Rust both sit at.

These assert something stricter: that this package parses each vector and
reproduces it byte for byte. A change to the Python types that silently stops
agreeing with Go now fails here.

The fixture is resolved relative to this file so the same path works under `pytest`
from the repository root and under Bazel, where it arrives through the
`//tests/integration/cross_language:golden_fixtures` filegroup in the target's
runfiles.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from libs.python.identifiers import ArtifactRef, Digest, ResourceId, ResourceVersion

FIXTURE = (
    Path(__file__).resolve().parents[4]
    / "tests/integration/cross_language/fixtures/primitives_v1.json"
)


@pytest.fixture(scope="module")
def vectors() -> dict[str, Any]:
    data: dict[str, Any] = json.loads(FIXTURE.read_text(encoding="utf-8"))
    assert data["schema"] == "mindclade-cross-language-primitives/v1"
    return data


def test_resource_id_vector(vectors: dict[str, Any]) -> None:
    identifier = ResourceId.parse(vectors["resource_id"])
    assert identifier.kind == vectors["resource_id_kind"]
    assert identifier.text == vectors["resource_id"]


def test_digest_vector(vectors: dict[str, Any]) -> None:
    assert Digest.parse(vectors["digest"]).text == vectors["digest"]


def test_resource_version_vector(vectors: dict[str, Any]) -> None:
    version = ResourceVersion.parse(vectors["resource_version"])
    assert version.text == vectors["resource_version"]
    # The version's digest is the same digest the fixture carries separately;
    # that binding is the whole point of the token.
    assert version.digest.text == vectors["digest"]


def test_artifact_ref_vector(vectors: dict[str, Any]) -> None:
    reference = ArtifactRef.from_document(vectors["artifact_ref"])
    assert reference.to_document() == vectors["artifact_ref"]
    assert reference.digest.text == vectors["digest"]
