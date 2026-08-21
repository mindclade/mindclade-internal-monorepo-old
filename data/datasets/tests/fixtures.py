# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Shared immutable dataset fixtures for hermetic catalog and resolver tests."""

from __future__ import annotations

import datetime as dt

from data.contracts import Sensitivity
from data.datasets import (
    DatasetVersionManifest,
    LineageEdge,
    LineageGraph,
    LineageNode,
    SplitPolicy,
)
from data.manifest import ArtifactRef

DIGESTS = tuple("sha256:" + character * 64 for character in "abcdef0123456789")


def manifest(version: str = "1.0.0") -> DatasetVersionManifest:
    """Build a content-addressed manifest without network or mutable state."""

    graph = LineageGraph(
        (LineageNode(DIGESTS[0], "raw"), LineageNode(DIGESTS[1], "model-ready")),
        (LineageEdge(DIGESTS[0], DIGESTS[1], "curate", "1.0.0", DIGESTS[2]),),
    )
    return DatasetVersionManifest(
        dataset_id="sequence-pretraining",
        version=version,
        owner="data-platform",
        intended_uses=("sequence language-model pretraining",),
        prohibited_uses=("clinical decision making",),
        source_snapshot_digests=(DIGESTS[0],),
        artifacts=(ArtifactRef(DIGESTS[1], 128, "application/x-parquet", "dataset-shard", "v1"),),
        schema_digest=DIGESTS[3],
        canonicalization_version="sequence-parser-1.0.0",
        curation_version="sequence-curation-1.0.0",
        tokenizer_version="protein-v1",
        split_policy=SplitPolicy("group-hash-v1", 7, ("subject_id",)),
        quality_report_digest=DIGESTS[4],
        lineage_graph_digest=graph.digest,
        build_provenance_digest=DIGESTS[5],
        generated_at=dt.datetime(2026, 8, 20, tzinfo=dt.UTC),
        classification=Sensitivity.PROPRIETARY_INTERNAL,
        evidence_digests=(DIGESTS[6],),
    )
