# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from libs.python.artifacts import ArtifactManifest, lineage_order, reference_bytes
from libs.python.identifiers import ArtifactRef


def artifact(content: bytes, kind: str = "dataset") -> ArtifactRef:
    return reference_bytes(
        content,
        media_type="application/octet-stream",
        logical_kind=kind,
    )


def test_manifest_round_trips_and_snapshots_annotations() -> None:
    annotations = {"producer": "curation"}
    parent = artifact(b"parent")
    manifest = ArtifactManifest(artifact(b"child"), (parent,), annotations)
    annotations["producer"] = "changed"

    assert manifest.annotations["producer"] == "curation"
    assert ArtifactManifest.from_document(manifest.to_document()) == manifest
    assert (
        manifest.canonical_bytes()
        == ArtifactManifest.from_document(manifest.to_document()).canonical_bytes()
    )
    assert manifest.digest.text.startswith("sha256:")


def test_manifest_rejects_duplicate_and_self_parents() -> None:
    child = artifact(b"child")
    parent = artifact(b"parent")
    with pytest.raises(ValueError, match="unique"):
        ArtifactManifest(child, (parent, parent))
    with pytest.raises(ValueError, match="itself"):
        ArtifactManifest(child, (child,))


def test_manifest_parent_order_does_not_change_identity() -> None:
    child = artifact(b"child")
    first = artifact(b"first")
    second = artifact(b"second")

    assert (
        ArtifactManifest(child, (first, second)).canonical_bytes()
        == ArtifactManifest(child, (second, first)).canonical_bytes()
    )


def test_lineage_is_deterministic_and_rejects_cycles_or_missing_parents() -> None:
    root = ArtifactManifest(artifact(b"root"))
    child = ArtifactManifest(artifact(b"child"), (root.artifact,))
    assert lineage_order((child, root)) == (root, child)

    external = ArtifactManifest(artifact(b"external-child"), (artifact(b"absent"),))
    assert lineage_order((external,)) == (external,)
    with pytest.raises(ValueError, match="omits"):
        lineage_order((external,), require_complete=True)

    first_ref = artifact(b"first")
    second_ref = artifact(b"second")
    first = ArtifactManifest(first_ref, (second_ref,))
    second = ArtifactManifest(second_ref, (first_ref,))
    with pytest.raises(ValueError, match="cycle"):
        lineage_order((first, second))
