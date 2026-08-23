#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce Mindclade Go dependency layers and production paved roads.

The checker is intentionally syntax-light: it reads canonical Go import blocks
and scans a small set of high-risk production patterns. Go compilation remains
the source of truth; this tool supplies fast repository architecture feedback.
"""

from __future__ import annotations

import argparse
import re
import sys
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

# The repository's own module path, as declared by go.mod. It is spelled out in the prefixes
# below as well; all of them have to move together. The module-path cutover updated those and
# left this pattern on `mindclade.internal`, so it matched nothing and every rule downstream of
# it passed vacuously — the failure mode a layering check cannot signal about itself.
IMPORT_RE = re.compile(r'"(go\.mindclade\.dev/[^"\n]+)"')
GO_STATEMENT_RE = re.compile(r"(?m)^\s*go\s+(?:func\s*\(|[A-Za-z_][A-Za-z0-9_./]*\s*\()")


@dataclass(frozen=True)
class Violation:
    path: Path
    message: str
    line: int = 0

    def render(self, root: Path) -> str:
        relative = self.path.relative_to(root)
        location = f":{self.line}" if self.line else ""
        return f"{relative}{location}: {self.message}"


def _parts(package: str) -> tuple[str, ...]:
    return tuple(part for part in package.strip("/").split("/") if part)


def layer_for(relative_package: str) -> int | None:
    """Return the declared libs/go layer for one package path."""
    parts = _parts(relative_package)
    if not parts:
        return None
    top = parts[0]
    if top in {"clock", "faults", "identifiers"}:
        return 0
    # `internal/rpcfaults` exists only to serve the Connect and gRPC adapters — its own doc
    # comment says so, and connectx, grpcx, and httpx are its only importers. LAYERS.md has
    # always placed it at Layer 4; this classifier said Layer 1, which quietly licensed the one
    # direction LAYERS.md forbids ("Lower layers never import Layer 4"): a Layer 1 contract or a
    # Layer 2 mechanism could have imported transport fault translation with no violation. The
    # blanket `internal` rule is gone with it, so a future `internal/<other>` package is
    # unclassified and fails closed below rather than inheriting a layer it never earned.
    if parts[:2] == ("internal", "rpcfaults"):
        return 4
    if top in {"audit", "idempotency"} and len(parts) > 1:
        return 3
    if top == "messaging":
        return 1 if len(parts) == 1 or parts[1].endswith("test") else 3
    if top in {"httpx", "connectx", "grpcx"}:
        return 4
    if top == "kubernetes":
        return 3
    if top == "coordination":
        if any(part in {"postgres", "memory"} for part in parts[2:]):
            return 3
        return 2
    if top == "storage":
        if len(parts) < 2:
            return 1
        if parts[1] in {"blob", "cache", "lease"}:
            return 1 if len(parts) == 2 or parts[2].endswith("test") else 3
        if parts[1] == "sql":
            if len(parts) == 2 or parts[2] in {"transaction", "sqltest"}:
                return 1
            return 3
    if top in {
        "auth",
        "audit",
        "idempotency",
        "messaging",
        "pagination",
        "requestmeta",
        "resourceversion",
        "signing",
    }:
        return 1
    if top in {"config", "observability", "retry", "servicekit"}:
        return 2
    return None


def package_for_file(root: Path, path: Path) -> str | None:
    try:
        relative = path.parent.relative_to(root / "libs/go")
    except ValueError:
        return None
    return relative.as_posix()


def target_lib_package(import_path: str) -> str | None:
    """Return the libs/go package an import names, or None if it names something else.

    None here means "not a libs/go package at all" — the standard library, a third-party
    module, or another repository tree. Those are correctly ignored by the layer rules.
    It never means "a libs/go package I could not classify"; that is `layer_for`'s None,
    and the two must not be conflated.
    """
    prefix = "go.mindclade.dev/libs/go/"
    if not import_path.startswith(prefix):
        return None
    return import_path[len(prefix) :]


def go_packages(root: Path) -> list[str]:
    """Return every libs/go package path that holds Go source, tests included."""
    packages: set[str] = set()
    for path in (root / "libs/go").rglob("*.go"):
        package = package_for_file(root, path)
        if package is not None:
            packages.add(package)
    return sorted(packages)


def unclassified_package_violations(root: Path) -> list[Violation]:
    """Fail closed on any libs/go package `layer_for` cannot place.

    `layer_for` recognizes a fixed set of package names and returns None for everything
    else, and the import rule below can only compare two known layers. Skipping the
    comparison on an unknown layer meant a newly admitted `libs/go/<name>` package had
    every one of its intra-libs/go edges validated by nothing — the gate stopped applying
    to precisely the newest, least-reviewed code, and printed "Go architecture check
    passed" while doing it. Nothing in the output separated "checked and clean" from
    "never checked". Classification is therefore mandatory: an unplaceable package is an
    error naming the package, not a silent pass.
    """
    violations: list[Violation] = []
    base = root / "libs/go"
    for package in go_packages(root):
        if layer_for(package) is not None:
            continue
        violations.append(
            Violation(
                base if package == "." else base / package,
                "libs/go package has no layer in check_go_layers.layer_for; classify it "
                "there and in libs/go/LAYERS.md before its imports can be checked",
            )
        )
    return violations


def import_violations(root: Path, files: Iterable[Path]) -> list[Violation]:
    violations: list[Violation] = []
    for path in files:
        if path.name.endswith("_test.go") or path.name.endswith(".pb.go"):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        source_package = package_for_file(root, path)
        if source_package is None:
            # libs/go must never depend on consumer layers.
            continue
        source_layer = layer_for(source_package)
        for import_path in IMPORT_RE.findall(text):
            if import_path.startswith(
                (
                    "go.mindclade.dev/control/",
                    "go.mindclade.dev/services/",
                    "go.mindclade.dev/operators/",
                    "go.mindclade.dev/workers/",
                )
            ):
                violations.append(
                    Violation(path, f"libs/go imports consumer package {import_path}")
                )
                continue
            target_package = target_lib_package(import_path)
            if target_package is None:
                continue
            if any(part.endswith("test") for part in _parts(target_package)):
                source_is_testonly = any(part.endswith("test") for part in _parts(source_package))
                if not source_is_testonly:
                    violations.append(
                        Violation(
                            path, f"production package imports test-only package {import_path}"
                        )
                    )
                continue
            target_layer = layer_for(target_package)
            if source_layer is None or target_layer is None:
                # Both sides are real libs/go packages here: `package_for_file` already
                # excluded files outside libs/go, and `target_lib_package` already excluded
                # standard-library and third-party imports. So an unknown layer is never
                # "nothing to check" — it is an edge this gate cannot evaluate, and the
                # previous `continue` reported it identically to a clean edge.
                # `unclassified_package_violations` names the package; this names each edge
                # that went unvalidated because of it, so the blast radius is visible.
                unplaceable = source_package if source_layer is None else target_package
                violations.append(
                    Violation(
                        path,
                        f"import {import_path} cannot be layer-checked: libs/go package "
                        f"{unplaceable} has no layer in check_go_layers.layer_for",
                    )
                )
                continue
            if target_layer > source_layer:
                violations.append(
                    Violation(
                        path,
                        f"layer {source_layer} package {source_package} imports higher layer {target_layer} package {target_package}",
                    )
                )
    return violations


def _line_number(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def paved_road_violations(root: Path) -> list[Violation]:
    violations: list[Violation] = []
    production_roots = [
        root / "services/control_plane",
        root / "control",
        root / "operators",
        root / "workers",
    ]
    checks = {
        "http.DefaultClient": "use libs/go/httpx or httpx/outbound instead of http.DefaultClient",
        "http.DefaultTransport": "use the standard httpx transport instead of http.DefaultTransport",
        "signal.Notify(": "service processes must delegate signal ownership to servicekit",
        "signal.NotifyContext(": "service processes must delegate signal ownership to servicekit",
        "uuid.New(": "use libs/go/identifiers instead of a direct UUID generator",
    }
    for base in production_roots:
        if not base.exists():
            continue
        for path in base.rglob("*.go"):
            if path.name.endswith("_test.go") or "/generated/" in path.as_posix():
                continue
            text = path.read_text(encoding="utf-8", errors="replace")
            for token, message in checks.items():
                start = text.find(token)
                if start >= 0:
                    violations.append(Violation(path, message, _line_number(text, start)))
            # Process code must use servicekit task ownership rather than
            # detached goroutines. Domain-pure packages may start no goroutines.
            match = GO_STATEMENT_RE.search(text)
            if match:
                violations.append(
                    Violation(
                        path,
                        "detached goroutine; register owned work through servicekit.TaskGroup",
                        _line_number(text, match.start()),
                    )
                )
    command_root = root / "services/control_plane/cmd"
    if command_root.exists():
        for path in command_root.glob("*/main.go"):
            text = path.read_text(encoding="utf-8", errors="replace")
            required = [
                '"go.mindclade.dev/services/control_plane/internal/bootstrap"',
                "bootstrap.Main(",
            ]
            for token in required:
                if token not in text:
                    violations.append(
                        Violation(path, f"control-plane command must contain {token}")
                    )
            if '"go.mindclade.dev/libs/go/servicekit"' in text or "servicekit.New(" in text:
                violations.append(
                    Violation(path, "command bypasses servicekit/production bootstrap")
                )
    return violations


def generic_package_violations(root: Path) -> list[Violation]:
    violations: list[Violation] = []
    forbidden = {"common", "helpers", "shared", "utils"}
    for directory in (root / "libs/go").rglob("*"):
        if directory.is_dir() and directory.name in forbidden:
            violations.append(
                Violation(directory, "generic Go dumping-ground package is prohibited")
            )
    return violations


def check(root: Path) -> list[Violation]:
    go_files = list((root / "libs/go").rglob("*.go"))
    return sorted(
        import_violations(root, go_files)
        + unclassified_package_violations(root)
        + paved_road_violations(root)
        + generic_package_violations(root),
        key=lambda value: (value.path.as_posix(), value.line, value.message),
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    root = args.repo.resolve()
    violations = check(root)
    for violation in violations:
        print(violation.render(root), file=sys.stderr)
    if violations:
        print(f"Go architecture check failed with {len(violations)} violation(s)", file=sys.stderr)
        return 1
    print("Go architecture check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
