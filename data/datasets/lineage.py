# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-addressed acyclic dataset lineage graph."""

from __future__ import annotations

import hashlib
import json
import re
from dataclasses import dataclass, field

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_KIND = re.compile(r"[a-z][a-z0-9._-]{0,127}")
_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}")


@dataclass(frozen=True, slots=True, order=True)
class LineageNode:
    digest: str
    kind: str
    schema_digest: str | None = None
    classification: str = "internal"

    def __post_init__(self) -> None:
        if not _DIGEST.fullmatch(self.digest):
            raise ValueError("lineage node digest is invalid")
        if not _KIND.fullmatch(self.kind):
            raise ValueError("lineage node kind is invalid")
        if self.schema_digest is not None and not _DIGEST.fullmatch(self.schema_digest):
            raise ValueError("lineage node schema digest is invalid")
        if not _KIND.fullmatch(self.classification):
            raise ValueError("lineage node classification is invalid")


@dataclass(frozen=True, slots=True, order=True)
class LineageEdge:
    parent_digest: str
    child_digest: str
    operation: str
    implementation_version: str
    parameters_digest: str

    def __post_init__(self) -> None:
        if self.parent_digest == self.child_digest:
            raise ValueError("lineage edge may not be a self-edge")
        for value in (self.parent_digest, self.child_digest, self.parameters_digest):
            if not _DIGEST.fullmatch(value):
                raise ValueError("lineage edge digest is invalid")
        if not _KIND.fullmatch(self.operation) or not _VERSION.fullmatch(
            self.implementation_version
        ):
            raise ValueError("lineage edge operation/version is invalid")


@dataclass(frozen=True, slots=True)
class LineageGraph:
    nodes: tuple[LineageNode, ...]
    edges: tuple[LineageEdge, ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        nodes = tuple(sorted(self.nodes))
        edges = tuple(sorted(self.edges))
        if not nodes or len(nodes) > 200_000 or len(edges) > 1_000_000:
            raise ValueError("lineage graph is outside bounds")
        identities = [node.digest for node in nodes]
        if len(set(identities)) != len(identities):
            raise ValueError("lineage node digests must be unique")
        known = set(identities)
        if any(edge.parent_digest not in known or edge.child_digest not in known for edge in edges):
            raise ValueError("lineage edge references an unknown node")
        if len(set(edges)) != len(edges):
            raise ValueError("lineage edges must be unique")
        _assert_acyclic(known, edges)
        object.__setattr__(self, "nodes", nodes)
        object.__setattr__(self, "edges", edges)

    def canonical_document(self) -> str:
        value = {
            "schema_version": 1,
            "nodes": [
                {
                    "digest": node.digest,
                    "kind": node.kind,
                    "schema_digest": node.schema_digest,
                    "classification": node.classification,
                }
                for node in self.nodes
            ],
            "edges": [
                {
                    "parent_digest": edge.parent_digest,
                    "child_digest": edge.child_digest,
                    "operation": edge.operation,
                    "implementation_version": edge.implementation_version,
                    "parameters_digest": edge.parameters_digest,
                }
                for edge in self.edges
            ],
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"

    @property
    def digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_document().encode()).hexdigest()


def _assert_acyclic(nodes: set[str], edges: tuple[LineageEdge, ...]) -> None:
    children: dict[str, list[str]] = {node: [] for node in nodes}
    indegree = dict.fromkeys(nodes, 0)
    for edge in edges:
        children[edge.parent_digest].append(edge.child_digest)
        indegree[edge.child_digest] += 1
    ready = sorted(node for node, degree in indegree.items() if degree == 0)
    visited = 0
    while ready:
        node = ready.pop(0)
        visited += 1
        for child in sorted(children[node]):
            indegree[child] -= 1
            if indegree[child] == 0:
                ready.append(child)
                ready.sort()
    if visited != len(nodes):
        raise ValueError("lineage graph must be acyclic")
