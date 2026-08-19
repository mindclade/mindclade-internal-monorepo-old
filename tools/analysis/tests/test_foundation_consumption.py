# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


graphs = load("go_import_graph", ROOT / "tools/analysis/go_import_graph.py")
consumption = load(
    "check_foundation_consumption", ROOT / "tools/analysis/check_foundation_consumption.py"
)


def write(root: Path, relative: str, text: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def module_fixture(root: Path) -> None:
    """A miniature module: one command, one contract, one adapter, one orphan."""
    write(root, "go.mod", "module example.test\n\ngo 1.26.0\n")
    write(
        root,
        "services/control_plane/cmd/registry/main.go",
        'package main\n\nimport (\n\t_ "github.com/lib/pq"\n\n'
        '\t"example.test/services/control_plane/internal/providers"\n)\n\n'
        "func main() { providers.Run() }\n",
    )
    write(
        root,
        "services/control_plane/internal/providers/providers.go",
        'package providers\n\nimport "example.test/libs/go/storage/blob/gcs"\n\n'
        "func Run() { _ = gcs.Name }\n",
    )
    write(root, "libs/go/storage/blob/gcs/store.go", 'package gcs\n\nconst Name = "gcs"\n')
    write(root, "libs/go/kubernetes/client/client.go", "package client\n")
    write(root, "services/control_plane/internal/bootstrap/profile.go", 'package bootstrap\n')


def test_import_graph_ignores_comments_and_test_files() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(root, "go.mod", "module example.test\n")
        write(
            root,
            "app/app.go",
            "// See example.test/libs/go/decoy for background.\n"
            'package app\n\nimport (\n\t// example.test/libs/go/other is not an edge\n'
            '\t"example.test/libs/go/real"\n)\n',
        )
        write(root, "app/app_test.go", 'package app\n\nimport "example.test/libs/go/testonly"\n')
        write(root, "libs/go/real/real.go", "package real\n")
        write(root, "libs/go/decoy/decoy.go", "package decoy\n")
        write(root, "libs/go/testonly/testonly.go", "package testonly\n")

        production = graphs.import_graph(root)
        assert production["example.test/app"] == frozenset({"example.test/libs/go/real"})

        with_tests = graphs.import_graph(root, True)
        assert with_tests["example.test/app"] == frozenset(
            {"example.test/libs/go/real", "example.test/libs/go/testonly"}
        )


def test_nested_modules_are_outside_the_graph() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        write(root, "go.mod", "module example.test\n")
        write(root, "sdk/go/go.mod", "module example.test/sdk\n")
        write(root, "sdk/go/sdk.go", "package sdk\n")
        write(root, "libs/go/real/real.go", "package real\n")
        assert "example.test/sdk/go" not in graphs.import_graph(root)


def test_generated_document_follows_the_import_graph() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        module_fixture(root)
        write(
            root,
            "services/control_plane/internal/bootstrap/profile.go",
            'package bootstrap\n\nconst (\n\tRoleRegistry Role = "registry"\n'
            '\tRoleAdmin    Role = "admin"\n)\n',
        )
        document = consumption.generate(root)
        assert document["roles"]["registry"] == ["libs/go/storage/blob/gcs"]
        # Declared but not built: no command directory exists for this role.
        assert document["roles"]["admin"] == []


def test_drift_is_reported_in_both_directions() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        module_fixture(root)
        write(
            root,
            "services/control_plane/internal/bootstrap/profile.go",
            'package bootstrap\n\nconst RoleRegistry Role = "registry"\n',
        )
        write(root, "libs/go/UNCONSUMED.toml", 'packages = []\n')
        write(
            root,
            consumption.CONSUMPTION_PATH,
            json.dumps({"schema_version": 1, "roles": {"registry": ["libs/go/kubernetes/client"]}}),
        )
        errors = consumption.check(root)
        assert any("links libs/go/storage/blob/gcs but does not declare it" in e for e in errors)
        assert any("declares libs/go/kubernetes/client but does not link it" in e for e in errors)


def test_orphan_requires_a_waiver_and_a_stale_waiver_is_rejected() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        module_fixture(root)
        write(
            root,
            "services/control_plane/internal/bootstrap/profile.go",
            'package bootstrap\n\nconst RoleRegistry Role = "registry"\n',
        )
        write(root, consumption.CONSUMPTION_PATH, json.dumps(consumption.generate(root)))

        write(root, "libs/go/UNCONSUMED.toml", "schema_version = 1\n")
        assert any(
            "libs/go/kubernetes/client: no in-module importer" in e
            for e in consumption.check(root)
        )

        write(
            root,
            "libs/go/UNCONSUMED.toml",
            'schema_version = 1\n\n[[waiver]]\nreason = "x"\n'
            'packages = ["libs/go/kubernetes/client"]\n',
        )
        assert consumption.check(root) == []

        write(
            root,
            "libs/go/UNCONSUMED.toml",
            'schema_version = 1\n\n[[waiver]]\nreason = "x"\n'
            'packages = ["libs/go/kubernetes/client", "libs/go/storage/blob/gcs"]\n',
        )
        assert any("now consumed; remove the waiver" in e for e in consumption.check(root))
