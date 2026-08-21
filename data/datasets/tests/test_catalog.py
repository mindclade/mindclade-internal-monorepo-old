# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import tomllib
from pathlib import Path

import pytest

from data.datasets import (
    DatasetCatalog,
    DatasetMixture,
    LineageEdge,
    LineageGraph,
    LineageNode,
    MixtureComponent,
    PublicationState,
    parse_dataset_manifest,
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


def test_checked_in_manifest_and_mixture_examples_are_consistent() -> None:
    root = Path(__file__).resolve().parents[1]
    manifests = {
        item.dataset_id: item
        for path in sorted((root / "manifests").glob("*.json"))
        for item in (parse_dataset_manifest(path.read_text(encoding="utf-8")),)
    }
    assert set(manifests) == {
        "biomolecular-complexes",
        "rollout-trajectories",
        "sequence-pretraining",
    }
    for manifest_item in manifests.values():
        assert "release" in manifest_item.prohibited_uses[0].lower()

    for path in sorted((root / "mixtures").glob("*.toml")):
        raw = tomllib.loads(path.read_text(encoding="utf-8"))
        components = tuple(
            MixtureComponent(
                str(item["dataset_id"]),
                str(item["version"]),
                str(item["manifest_digest"]),
                str(item["split"]),
                float(item["weight"]),
            )
            for item in raw["components"]
        )
        mixture = DatasetMixture(str(raw["name"]), components, int(raw["seed"]))
        assert all(
            component.manifest_digest == manifests[component.dataset_id].manifest_digest
            for component in mixture.components
        )

    with pytest.raises(ValueError, match="duplicate field"):
        parse_dataset_manifest('{"schema_version":1,"schema_version":1}')
