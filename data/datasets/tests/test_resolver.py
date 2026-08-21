# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from data.datasets import DatasetCatalog, DatasetResolver
from data.datasets.tests.fixtures import DIGESTS, manifest
from data.manifest import ArtifactLocation, ArtifactRef


class Locations:
    def __init__(self, digest: str) -> None:
        self.digest = digest

    def locations(self, artifact: ArtifactRef) -> tuple[ArtifactLocation, ...]:
        return (
            ArtifactLocation(
                self.digest,
                "gcs",
                "gs://mindclade-data/datasets/sequence-pretraining/shard-00000.parquet",
                "1700000000000000",
                "us-central1",
            ),
        )


def test_resolver_binds_exact_manifest_artifacts_to_generations() -> None:
    item = manifest()
    resolved = DatasetResolver(
        DatasetCatalog((item,)), Locations(item.artifacts[0].digest)
    ).resolve(item.dataset_id, item.version)
    assert resolved.manifest.manifest_digest == item.manifest_digest
    assert resolved.artifacts[0].locations[0].generation == "1700000000000000"


def test_resolver_rejects_mismatched_and_missing_locations() -> None:
    item = manifest()
    with pytest.raises(ValueError, match="different digest"):
        DatasetResolver(DatasetCatalog((item,)), Locations(DIGESTS[9])).resolve(
            item.dataset_id, item.version
        )

    class Missing:
        def locations(self, artifact: ArtifactRef) -> tuple[ArtifactLocation, ...]:
            return ()

    with pytest.raises(LookupError, match="no registered location"):
        DatasetResolver(DatasetCatalog((item,)), Missing()).resolve(item.dataset_id, item.version)
