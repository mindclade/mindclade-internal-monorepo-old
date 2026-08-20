#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Derive foundation consumption from the Go import graph.

The control-plane consumption matrix used to be a hand-written table of package
name strings. A table like that cannot fail: it stated that the API role
consumes libs/go/audit/postgres while no Go file imported it, and nothing
compared the two. This checker removes the hand-written half.

    consumption.json  is generated from the import graph and embedded by
                      bootstrap, so --describe-profile reports what a binary
                      actually links.
    UNCONSUMED.toml   records every libs/go package that nothing imports, so an
                      orphaned foundation package is a reviewed decision rather
                      than an accident.

Regenerate with `python3 tools/analysis/check_foundation_consumption.py --write`.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import tomllib
from pathlib import Path

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import go_import_graph as graphs  # noqa: E402  (path is established immediately above)

CONSUMPTION_PATH = "services/control_plane/internal/bootstrap/consumption.json"
WAIVER_PATH = "libs/go/UNCONSUMED.toml"
COMMAND_ROOT = "services/control_plane/cmd"
PROFILE_PATH = "services/control_plane/internal/bootstrap/profile.go"
ROLE_RE = re.compile(r'(?m)^\s*(?:const\s+)?Role[A-Za-z]+\s+Role\s*=\s*"([a-z0-9-]+)"')
SCHEMA_VERSION = 1


def declared_roles(root: Path) -> list[str]:
    return sorted(set(ROLE_RE.findall((root / PROFILE_PATH).read_text(encoding="utf-8"))))


def command_for(role: str) -> str:
    return role.replace("-", "_")


def generate(root: Path) -> dict:
    """Return the consumption document implied by the current import graph."""
    module = graphs.module_path(root)
    graph = graphs.import_graph(root)
    roles: dict[str, list[str]] = {}
    for role in declared_roles(root):
        directory = root / COMMAND_ROOT / command_for(role)
        if not directory.is_dir():
            roles[role] = []
            continue
        package = f"{module}/{COMMAND_ROOT}/{command_for(role)}"
        reachable = graphs.transitive_imports(graph, [package])
        roles[role] = graphs.foundation_packages(module, reachable)
    return {"schema_version": SCHEMA_VERSION, "roles": roles}


def render(document: dict) -> str:
    return json.dumps(document, indent=2, sort_keys=True) + "\n"


def orphans(root: Path) -> list[str]:
    """Return libs/go packages that nothing in the module imports."""
    module = graphs.module_path(root)
    graph = graphs.import_graph(root, True)
    imported: set[str] = set()
    for edges in graph.values():
        imported |= set(edges)
    prefix = f"{module}/libs/go/"
    return sorted(
        package[len(module) + 1 :]
        for package in graph
        if package.startswith(prefix) and package not in imported
    )


def waived(root: Path) -> set[str]:
    document = tomllib.loads((root / WAIVER_PATH).read_text(encoding="utf-8"))
    values: set[str] = set()
    for waiver in document.get("waiver", []):
        values.update(waiver.get("packages", []))
    return values


def check(root: Path) -> list[str]:
    errors: list[str] = []

    expected = generate(root)
    path = root / CONSUMPTION_PATH
    if not path.exists():
        return [f"{CONSUMPTION_PATH}: missing; run check_foundation_consumption.py --write"]
    actual = json.loads(path.read_text(encoding="utf-8"))
    if actual.get("schema_version") != SCHEMA_VERSION:
        errors.append(f"{CONSUMPTION_PATH}: unsupported schema_version")
    for role in sorted(set(expected["roles"]) | set(actual.get("roles", {}))):
        want = expected["roles"].get(role)
        got = actual.get("roles", {}).get(role)
        if want is None:
            errors.append(f"{CONSUMPTION_PATH}: role {role!r} is not a declared process role")
            continue
        if got is None:
            errors.append(f"{CONSUMPTION_PATH}: role {role!r} is missing")
            continue
        for package in sorted(set(want) - set(got)):
            errors.append(f"{CONSUMPTION_PATH}: {role} links {package} but does not declare it")
        for package in sorted(set(got) - set(want)):
            errors.append(f"{CONSUMPTION_PATH}: {role} declares {package} but does not link it")

    found = set(orphans(root))
    allowed = waived(root)
    for package in sorted(found - allowed):
        errors.append(f"{package}: no in-module importer and no waiver in {WAIVER_PATH}")
    for package in sorted(allowed - found):
        errors.append(
            f"{package}: waived in {WAIVER_PATH} but no longer unconsumed "
            "(it gained an importer, or it was removed); drop the waiver"
        )
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--write", action="store_true", help="regenerate the consumption document")
    args = parser.parse_args(argv)
    root = args.repo.resolve()
    if args.write:
        (root / CONSUMPTION_PATH).write_text(render(generate(root)), encoding="utf-8")
        print(f"wrote {CONSUMPTION_PATH}")
        return 0
    errors = check(root)
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(
            f"foundation consumption check failed with {len(errors)} finding(s)",
            file=sys.stderr,
        )
        return 1
    print("foundation consumption check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
