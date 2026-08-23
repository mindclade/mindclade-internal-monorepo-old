# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Every in-tree projection of ``mindclade.common.v1.ArtifactRef`` must agree with the wire.

``protocols/`` is the declared wire authority, but nothing compiled the hand-written
``ArtifactRef`` copies against it, so they drifted. ``data/manifest.py`` typed
``schema_version`` as ``str`` and validated it against a lowercase-token regex, while the proto
declares ``uint32`` and ``libs/python/identifiers/artifact.py`` documents the integer as the
settled answer -- a ``"v1"`` from a dataset manifest could never round-trip through the wire
field it claimed to be.

The gate is deliberately *discovery-based*: it finds every class named ``ArtifactRef`` under the
source roots rather than checking a list. A sixth copy therefore fails on the day it is written
instead of on the day someone audits the tree. Go's projection is named ``artifacts.Ref``
(package-qualified, so the ``Artifact`` prefix would stutter) and is pinned by path.
"""

from __future__ import annotations

import ast
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
PROTO = ROOT / "protocols/proto/mindclade/common/v1/artifact_ref.proto"

# Directories that hold hand-written source. Generated trees are excluded because they are
# projections of the proto by construction and are checked by the descriptor governance test.
PYTHON_ROOTS = ("libs/python", "data", "preprocessing", "serving", "training", "evaluation")
EXCLUDED_PARTS = frozenset({"gen", "generated", "node_modules", ".venv", "__pycache__"})

GO_PROJECTION = ROOT / "control/artifacts/catalog.go"
GO_TYPE = "Ref"

# One protobuf scalar admits more than one faithful host spelling: a validated newtype over a
# string is stricter than `str`, not different from it. Anything not listed here is a conflict.
PYTHON_TYPES = {
    "string": frozenset({"str", "Digest"}),
    "uint32": frozenset({"int"}),
    "uint64": frozenset({"int"}),
}
GO_TYPES = {
    "string": frozenset({"string", "identifiers.Digest"}),
    "uint32": frozenset({"uint32"}),
    "uint64": frozenset({"uint64"}),
}

_MESSAGE = re.compile(r"message\s+ArtifactRef\s*\{(.*?)\n\}", re.DOTALL)
_FIELD = re.compile(r"^\s*([\w.]+)\s+(\w+)\s*=\s*\d+\s*;")
_GO_STRUCT = re.compile(r"type\s+" + GO_TYPE + r"\s+struct\s*\{(.*?)\n\}", re.DOTALL)
_GO_FIELD = re.compile(r"^\s*(\w+)\s+([\w.]+)")


def proto_fields() -> dict[str, str]:
    """Field name -> declared protobuf scalar type for the canonical message."""

    text = re.sub(r"//.*", "", PROTO.read_text())
    body = _MESSAGE.search(text)
    assert body, f"{PROTO} no longer declares message ArtifactRef"
    fields = {}
    for line in body.group(1).splitlines():
        match = _FIELD.match(line)
        if match:
            fields[match.group(2)] = match.group(1)
    assert fields, "parsed no fields out of ArtifactRef; the parser has gone stale"
    return fields


def _snake(name: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()


def python_projections() -> dict[str, dict[str, str]]:
    """Relative path -> {field: annotation} for every hand-written ArtifactRef class."""

    found: dict[str, dict[str, str]] = {}
    for root in PYTHON_ROOTS:
        for path in sorted((ROOT / root).rglob("*.py")):
            if EXCLUDED_PARTS & set(path.relative_to(ROOT).parts):
                continue
            source = path.read_text()
            if "class ArtifactRef" not in source:
                continue
            for node in ast.walk(ast.parse(source, filename=str(path))):
                if not isinstance(node, ast.ClassDef) or node.name != "ArtifactRef":
                    continue
                found[str(path.relative_to(ROOT))] = {
                    statement.target.id: ast.unparse(statement.annotation)
                    for statement in node.body
                    if isinstance(statement, ast.AnnAssign)
                    and isinstance(statement.target, ast.Name)
                }
    assert found, "found no Python ArtifactRef projections; the discovery walk is broken"
    return found


def go_projection() -> dict[str, str]:
    text = GO_PROJECTION.read_text()
    body = _GO_STRUCT.search(text)
    assert body, f"{GO_PROJECTION} no longer declares `type {GO_TYPE} struct`"
    fields = {}
    for line in body.group(1).splitlines():
        match = _GO_FIELD.match(line)
        if match:
            fields[_snake(match.group(1))] = match.group(2)
    return fields


@pytest.mark.parametrize("path", sorted(python_projections()))
def test_python_artifact_ref_agrees_with_the_wire(path: str) -> None:
    wire = proto_fields()
    projection = python_projections()[path]

    assert set(projection) == set(wire), (
        f"{path}: ArtifactRef field set diverges from mindclade.common.v1.ArtifactRef: "
        f"only-here={sorted(set(projection) - set(wire))} "
        f"only-wire={sorted(set(wire) - set(projection))}"
    )
    conflicts = [
        f"{field}: declared {annotation!r} but the wire declares {wire[field]} "
        f"(accepted: {sorted(PYTHON_TYPES[wire[field]])})"
        for field, annotation in sorted(projection.items())
        if annotation not in PYTHON_TYPES[wire[field]]
    ]
    assert not conflicts, f"{path}: ArtifactRef contradicts the wire contract: {conflicts}"


def test_go_artifact_ref_agrees_with_the_wire() -> None:
    wire = proto_fields()
    projection = go_projection()

    assert set(projection) == set(wire), (
        f"{GO_PROJECTION.relative_to(ROOT)}: field set diverges from the wire: "
        f"only-here={sorted(set(projection) - set(wire))} "
        f"only-wire={sorted(set(wire) - set(projection))}"
    )
    conflicts = [
        f"{field}: declared {declared!r} but the wire declares {wire[field]}"
        for field, declared in sorted(projection.items())
        if declared not in GO_TYPES[wire[field]]
    ]
    assert not conflicts, f"Go ArtifactRef contradicts the wire contract: {conflicts}"


def test_every_wire_scalar_has_a_declared_host_spelling() -> None:
    # Without this, adding `bytes` or `int64` to the proto would make the checks above pass
    # vacuously by KeyError-free omission the next time the mapping tables are edited.
    undeclared = sorted(set(proto_fields().values()) - set(PYTHON_TYPES))
    assert not undeclared, (
        f"ArtifactRef gained protobuf scalar types with no declared host spelling: {undeclared}"
    )
    assert set(PYTHON_TYPES) == set(GO_TYPES), (
        "the Python and Go scalar tables disagree about which wire types exist"
    )
