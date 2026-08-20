# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic validation of bounded artifact provenance graphs."""

from __future__ import annotations

import heapq
from collections.abc import Iterable
from itertools import islice
from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted

from .manifest import ArtifactManifest

MAXIMUM_LINEAGE_NODES: Final = 4096
MAXIMUM_LINEAGE_EDGES: Final = 65536


def lineage_order(
    manifests: Iterable[ArtifactManifest], *, require_complete: bool = False
) -> tuple[ArtifactManifest, ...]:
    """Return parents-before-children order, rejecting cycles and ambiguity."""
    try:
        iterator = iter(manifests)
    except TypeError as error:
        raise InvalidArgument(
            "artifact lineage must be iterable",
            reason="artifact_lineage_type",
            cause=error,
        ) from error
    materialized = tuple(islice(iterator, MAXIMUM_LINEAGE_NODES + 1))
    if len(materialized) > MAXIMUM_LINEAGE_NODES:
        raise ResourceExhausted(
            f"artifact lineage accepts at most {MAXIMUM_LINEAGE_NODES} nodes",
            reason="artifact_lineage_nodes",
        )
    if not isinstance(require_complete, bool):
        raise InvalidArgument(
            "require_complete must be a boolean",
            reason="artifact_lineage_option",
        )
    if any(not isinstance(manifest, ArtifactManifest) for manifest in materialized):
        raise InvalidArgument(
            "artifact lineage must contain ArtifactManifest values",
            reason="artifact_lineage_node",
        )
    by_digest = {manifest.artifact.digest.text: manifest for manifest in materialized}
    if len(by_digest) != len(materialized):
        raise InvalidArgument(
            "artifact lineage contains duplicate artifact identities",
            reason="artifact_lineage_duplicate",
        )

    indegree = dict.fromkeys(by_digest, 0)
    children: dict[str, list[str]] = {digest: [] for digest in by_digest}
    edge_count = 0
    for child_digest, manifest in by_digest.items():
        for parent in manifest.parents:
            parent_digest = parent.digest.text
            if parent_digest not in by_digest:
                if require_complete:
                    raise InvalidArgument(
                        "artifact lineage omits a referenced parent",
                        reason="artifact_lineage_missing_parent",
                    )
                continue
            edge_count += 1
            if edge_count > MAXIMUM_LINEAGE_EDGES:
                raise ResourceExhausted(
                    f"artifact lineage accepts at most {MAXIMUM_LINEAGE_EDGES} edges",
                    reason="artifact_lineage_edges",
                )
            indegree[child_digest] += 1
            children[parent_digest].append(child_digest)

    ready = [digest for digest, degree in indegree.items() if degree == 0]
    heapq.heapify(ready)
    ordered: list[ArtifactManifest] = []
    while ready:
        digest = heapq.heappop(ready)
        ordered.append(by_digest[digest])
        for child in sorted(children[digest]):
            indegree[child] -= 1
            if indegree[child] == 0:
                heapq.heappush(ready, child)
    if len(ordered) != len(materialized):
        raise InvalidArgument(
            "artifact lineage contains a cycle",
            reason="artifact_lineage_cycle",
        )
    return tuple(ordered)
