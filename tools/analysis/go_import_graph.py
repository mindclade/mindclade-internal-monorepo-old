#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Build the repository's Go import graph from source.

The graph answers one question the declared architecture cannot: what does a
binary actually link? Package documentation, capability tables, and hand-written
inventories all drift from the build; import edges cannot.

Nested modules are excluded because their packages are not part of this build.
Test files are excluded because a binary does not link them.
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from functools import lru_cache
from pathlib import Path

MODULE_RE = re.compile(r"(?m)^module\s+(\S+)\s*$")
IMPORT_BLOCK_RE = re.compile(r"(?ms)^import\s*\(\s*(.*?)^\s*\)")
IMPORT_SINGLE_RE = re.compile(r'(?m)^import\s+(?:[A-Za-z0-9_.]+\s+)?"([^"]+)"')
QUOTED_RE = re.compile(r'"([^"]+)"')
LINE_COMMENT_RE = re.compile(r"//.*$", re.M)

SKIP_DIRECTORIES = {".git", "node_modules", "target", "__pycache__", ".venv", "vendor"}


def module_path(root: Path) -> str:
    """Return the module path declared by the repository-root go.mod."""
    match = MODULE_RE.search((root / "go.mod").read_text(encoding="utf-8"))
    if not match:
        raise ValueError(f"{root}/go.mod does not declare a module path")
    return match.group(1)


def nested_module_directories(root: Path) -> set[Path]:
    """Return directories that declare their own module and are therefore
    outside this module's build."""
    return {path.parent for path in root.rglob("go.mod") if path.parent != root}


def file_imports(text: str) -> set[str]:
    """Return the import paths declared by one Go source file.

    Only import declarations are read. Package paths that appear in comments,
    string constants, or embedded documents are deliberately not edges.
    """
    imports: set[str] = set()
    for block in IMPORT_BLOCK_RE.findall(text):
        imports.update(QUOTED_RE.findall(LINE_COMMENT_RE.sub("", block)))
    imports.update(IMPORT_SINGLE_RE.findall(text))
    return imports


def _go_files(directory: Path, include_tests: bool) -> list[Path]:
    return sorted(
        path
        for path in directory.glob("*.go")
        if (include_tests or not path.name.endswith("_test.go")) and not path.name.endswith(".pb.go")
    )


@lru_cache(maxsize=None)
def import_graph(root: Path, include_tests: bool = False) -> dict[str, frozenset[str]]:
    """Return {package import path: in-module packages it imports}.

    include_tests answers a different question: a binary links only production
    files, but a package that exists solely to be exercised by tests is still
    consumed. Reachability uses the production graph; orphan detection uses the
    graph with tests.
    """
    module = module_path(root)
    excluded = nested_module_directories(root)
    graph: dict[str, frozenset[str]] = {}
    for directory in sorted(root.rglob("*")):
        if not directory.is_dir():
            continue
        parts = set(directory.relative_to(root).parts)
        if parts & SKIP_DIRECTORIES:
            continue
        if any(directory == item or item in directory.parents for item in excluded):
            continue
        files = _go_files(directory, include_tests)
        if not files:
            continue
        relative = directory.relative_to(root).as_posix()
        package = module if relative == "." else f"{module}/{relative}"
        edges: set[str] = set()
        for path in files:
            for value in file_imports(path.read_text(encoding="utf-8", errors="replace")):
                if value == module or value.startswith(module + "/"):
                    edges.add(value)
        graph[package] = frozenset(edges)
    return graph


def transitive_imports(graph: dict[str, frozenset[str]], roots: Iterable[str]) -> set[str]:
    """Return every in-module package reachable from roots, excluding roots."""
    seen: set[str] = set()
    pending = [value for value in roots]
    while pending:
        current = pending.pop()
        for value in graph.get(current, frozenset()):
            if value in seen:
                continue
            seen.add(value)
            pending.append(value)
    return seen


def foundation_packages(module: str, packages: Iterable[str]) -> list[str]:
    """Return the libs/go packages in packages, as repository-relative paths."""
    prefix = f"{module}/libs/go/"
    return sorted(value[len(module) + 1 :] for value in packages if value.startswith(prefix))
