# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from preprocessing.cache.keys import msa_search_key
from preprocessing.contracts import ArtifactRef
from preprocessing.pipeline import plan_structure_pipeline
from preprocessing.provenance import DatabaseSnapshot, Manifest


def test_structure_plan_and_cache_are_deterministic():
    a = ArtifactRef("sha256:" + "a" * 64, 10, "application/json", "input", 1)
    p = plan_structure_pipeline(
        prefix="job1",
        input_artifact=a,
        config_digest="sha256:" + "b" * 64,
        reference_snapshot_digest="sha256:" + "c" * 64,
    )
    assert p.stages[-1].spec.stage_id == "job1:features"
    x = msa_search_key(
        "sha256:" + "d" * 64, "mmseqs", "1", "sha256:" + "e" * 64, "sha256:" + "f" * 64
    )
    y = msa_search_key(
        "sha256:" + "d" * 64, "mmseqs", "1", "sha256:" + "e" * 64, "sha256:" + "f" * 64
    )
    assert x == y


def test_provenance_manifest_digest_changes_with_database():
    db = DatabaseSnapshot(
        "refdb_x",
        "uniref",
        "1",
        "sha256:" + "1" * 64,
        "2026-01-01",
        "mmseqs",
        "mmseqs",
        "1",
        ("sha256:" + "2" * 64,),
    )
    m = Manifest(
        1, "p1", "sha256:" + "3" * 64, ("sha256:" + "4" * 64,), (db,), (), (), "sha256:" + "5" * 64
    )
    assert m.digest.startswith("sha256:")
