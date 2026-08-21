# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from data.datasets import (
    DatasetCatalog,
    DatasetMixture,
    LineageEdge,
    LineageGraph,
    LineageNode,
    MixtureComponent,
    PublicationState,
    validate_transition,
)
from data.datasets.tests.fixtures import DIGESTS, manifest


def test_catalog_resolves_only_exact_immutable_versions() -> None:
    first = manifest()
    second = manifest("1.1.0")
    catalog = DatasetCatalog((second, first))
    assert catalog.resolve("sequence-pretraining", "1.0.0").manifest_digest == first.manifest_digest
    assert [item.version for item in catalog.versions("sequence-pretraining")] == [
        "1.0.0",
        "1.1.0",
    ]
    with pytest.raises(KeyError, match="exact"):
        catalog.resolve("sequence-pretraining", "latest")


def test_lineage_rejects_cycles_and_manifest_digest_is_stable() -> None:
    nodes = (LineageNode(DIGESTS[0], "raw"), LineageNode(DIGESTS[1], "curated"))
    with pytest.raises(ValueError, match="acyclic"):
        LineageGraph(
            nodes,
            (
                LineageEdge(DIGESTS[0], DIGESTS[1], "curate", "v1", DIGESTS[2]),
                LineageEdge(DIGESTS[1], DIGESTS[0], "reverse", "v1", DIGESTS[3]),
            ),
        )
    assert manifest().manifest_digest == manifest().manifest_digest


def test_publication_state_and_mixture_fail_closed() -> None:
    validate_transition(PublicationState.QUALIFIED, PublicationState.PUBLISHED)
    with pytest.raises(ValueError, match="invalid"):
        validate_transition(PublicationState.DRAFT, PublicationState.PUBLISHED)
    component = MixtureComponent(
        "sequence-pretraining", "1.0.0", manifest().manifest_digest, "train", 1.0
    )
    assert DatasetMixture("pretraining", (component,), 7).digest.startswith("sha256:")
    with pytest.raises(ValueError, match="sum"):
        DatasetMixture(
            "bad",
            (
                MixtureComponent("a", "1.0.0", DIGESTS[0], "train", 0.2),
                MixtureComponent("b", "1.0.0", DIGESTS[1], "train", 0.2),
            ),
            7,
        )
