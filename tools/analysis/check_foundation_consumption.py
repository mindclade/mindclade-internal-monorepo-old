#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Derive foundation consumption from the Go import graph.

The control-plane consumption matrix used to be a hand-written table of package
name strings. A table like that cannot fail: it stated that the API role
consumes libs/go/audit/postgres while no Go file imported it, and nothing
compared the two.

    consumption.json  is generated from the import graph and embedded by
                      bootstrap, so --describe-profile reports what a binary
                      actually links.
    UNCONSUMED.toml   records every libs/go package that nothing imports, so an
                      orphaned foundation package is a reviewed decision rather
                      than an accident.
    CONSUMPTION.md    still carries a hand-written matrix, because intent is not
                      derivable from imports. That half went unchecked for long
                      enough to accumulate 24 wrong cells: `resourceversion`
                      claimed "required" for three roles that link nothing under
                      it, `pagination` and `storage/sql/migrate` claimed "no" for
                      roles whose binaries link them through a shared provider
                      package, and a merged dispatcher column asserted
                      "required for webhooks" against a binary with no HTTP
                      client. Every cell now starts with a token this checker
                      enforces against the generated inventory, so intent can
                      still be wrong but can no longer be silently wrong.

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
MATRIX_PATH = "libs/go/CONSUMPTION.md"
COMMAND_ROOT = "services/control_plane/cmd"
PROFILE_PATH = "services/control_plane/internal/bootstrap/profile.go"
ROLE_RE = re.compile(r'(?m)^\s*(?:const\s+)?Role[A-Za-z]+\s+Role\s*=\s*"([a-z0-9-]+)"')
SCHEMA_VERSION = 1

MATRIX_HEADING = "## Process consumption matrix"
MATRIX_HEADER_CELL = "Mechanism"
BACKTICK_RE = re.compile(r"`([^`]+)`")
MATERIALIZED_RE = re.compile(r"All (\d+) declared roles are materialized")

# The token that opens a matrix cell, and what the import graph must show for it.
# `optional` is deliberately unenforced: it means discretionary, and pretending
# otherwise would push editors toward prose that no checker can read at all.
TOKEN_MUST_LINK = {
    "required": True,
    "no": False,
    "transitive": True,
    "unmaterialized": False,
}
TOKENS = set(TOKEN_MUST_LINK) | {"optional"}
# libs/go/internal/* is not a mechanism a consumer may select, so it is not
# required to appear as a matrix row.
MATRIX_EXEMPT_PREFIX = "libs/go/internal/"


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


def waiver_hygiene(root: Path) -> list[str]:
    """Reject waivers that record no debt.

    A waiver without a reason is an exemption wearing a debt record's clothes,
    and the same package listed twice lets one block be deleted while the
    package stays silently waived by the other.
    """
    document = tomllib.loads((root / WAIVER_PATH).read_text(encoding="utf-8"))
    errors: list[str] = []
    seen: set[str] = set()
    for index, waiver in enumerate(document.get("waiver", [])):
        packages = waiver.get("packages", [])
        if not packages:
            errors.append(f"{WAIVER_PATH}: waiver #{index} lists no packages")
        if not waiver.get("reason", "").strip():
            errors.append(f"{WAIVER_PATH}: waiver #{index} has no reason")
        for package in packages:
            if package in seen:
                errors.append(f"{WAIVER_PATH}: {package} is waived more than once")
            seen.add(package)
    return errors


def matrix_table(root: Path) -> tuple[list[str] | None, list[list[str]], str | None]:
    """Return (header cells, body rows, error) for the process consumption matrix.

    The section holds a token legend table as well, so the matrix is identified
    by its first header cell rather than by position. A missing heading or table
    is returned as an error rather than raised: this runs inside
    run_architecture_checks, whose CHECKS loop has no exception handler, so an
    exception here would abandon every checker queued behind it and report a
    traceback instead of a finding.
    """
    lines = (root / MATRIX_PATH).read_text(encoding="utf-8").splitlines()
    if MATRIX_HEADING not in lines:
        return None, [], f"{MATRIX_PATH}: no {MATRIX_HEADING!r} section"
    start = lines.index(MATRIX_HEADING)
    header: list[str] | None = None
    rows: list[list[str]] = []
    for line in lines[start + 1 :]:
        if line.startswith("## "):
            break
        if not line.startswith("|"):
            continue
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if header is None:
            if cells and cells[0] == MATRIX_HEADER_CELL:
                header = cells
            continue
        if set("".join(cells)) <= set("-: "):
            continue
        rows.append(cells)
    if header is None:
        return None, [], f"{MATRIX_PATH}: matrix table not found under {MATRIX_HEADING!r}"
    return header, rows, None


def attribute(package: str, prefix_sets: list[list[str]]) -> int | None:
    """Return the index of the most specific matrix row that owns a package.

    Longest prefix wins so that a `coordination` row and a `coordination/outbox`
    row can coexist and mean exactly what each says.
    """
    best: tuple[int, int] | None = None
    for index, prefixes in enumerate(prefix_sets):
        for prefix in prefixes:
            matches = package == prefix or package.startswith(prefix + "/")
            if matches and (best is None or len(prefix) > best[1]):
                best = (index, len(prefix))
    return None if best is None else best[0]


def check_matrix(root: Path, inventory: dict[str, list[str]]) -> list[str]:
    """Verify every enforced cell of the CONSUMPTION.md matrix against imports."""
    errors: list[str] = []
    header, rows, failure = matrix_table(root)
    if failure is not None or header is None:
        return [failure or f"{MATRIX_PATH}: matrix table not found"]
    roles = set(declared_roles(root))

    columns: list[list[str]] = []
    claimed: dict[str, int] = {}
    for index, cell in enumerate(header[1:]):
        named = BACKTICK_RE.findall(cell)
        if not named:
            errors.append(f"{MATRIX_PATH}: matrix column {index + 1} names no role")
        for role in named:
            if role not in roles:
                errors.append(f"{MATRIX_PATH}: column {index + 1} names unknown role {role!r}")
            if role in claimed:
                errors.append(f"{MATRIX_PATH}: role {role!r} appears in more than one column")
            claimed[role] = index
        columns.append(named)
    for role in sorted(roles - set(claimed)):
        errors.append(f"{MATRIX_PATH}: declared role {role!r} has no matrix column")

    packages = set(
        graphs.foundation_packages(graphs.module_path(root), graphs.import_graph(root, True))
    )
    prefix_sets: list[list[str]] = []
    for row in rows:
        prefixes = [f"libs/go/{name}" for name in BACKTICK_RE.findall(row[0])]
        if not prefixes:
            errors.append(f"{MATRIX_PATH}: matrix row {row[0]!r} names no package")
        for prefix in prefixes:
            if prefix not in packages:
                errors.append(
                    f"{MATRIX_PATH}: row {row[0]!r} names {prefix}, which is not a Go package"
                )
        prefix_sets.append(prefixes)

    for index, row in enumerate(rows):
        if len(row) != len(header):
            errors.append(
                f"{MATRIX_PATH}: row {row[0]!r} has {len(row)} cells, expected {len(header)}"
            )
            continue
        for named, cell in zip(columns, row[1:], strict=True):
            token = cell.split()[0].lower() if cell.split() else ""
            if token not in TOKENS:
                errors.append(
                    f"{MATRIX_PATH}: row {row[0]!r} cell {cell!r} does not start with one of "
                    + ", ".join(sorted(TOKENS))
                )
                continue
            if token == "optional":
                continue
            for role in named:
                owned = [
                    package
                    for package in inventory.get(role, [])
                    if attribute(package, prefix_sets) == index
                ]
                if TOKEN_MUST_LINK[token] and not owned:
                    errors.append(
                        f"{MATRIX_PATH}: row {row[0]!r} says {token!r} for role {role!r}, "
                        "but that role links nothing under it"
                    )
                if not TOKEN_MUST_LINK[token] and owned:
                    errors.append(
                        f"{MATRIX_PATH}: row {row[0]!r} says {token!r} for role {role!r}, "
                        f"but that role links {', '.join(owned)}"
                    )

    linked = {package for values in inventory.values() for package in values}
    for package in sorted(linked):
        if package.startswith(MATRIX_EXEMPT_PREFIX):
            continue
        if attribute(package, prefix_sets) is None:
            errors.append(
                f"{MATRIX_PATH}: {package} is linked by a control-plane binary "
                "but no matrix row declares a policy for it"
            )

    materialized = MATERIALIZED_RE.search((root / MATRIX_PATH).read_text(encoding="utf-8"))
    if materialized is None:
        errors.append(f"{MATRIX_PATH}: no 'All <n> declared roles are materialized' statement")
    elif int(materialized.group(1)) != len(roles):
        errors.append(
            f"{MATRIX_PATH}: claims {materialized.group(1)} declared roles; profile.go declares {len(roles)}"
        )
    return errors


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

    # A role declared in profile.go with no command directory generates an empty
    # inventory, which silently satisfies every check below. Say so instead.
    for role in declared_roles(root):
        if not (root / COMMAND_ROOT / command_for(role)).is_dir():
            errors.append(
                f"{PROFILE_PATH}: role {role!r} is declared but has no "
                f"{COMMAND_ROOT}/{command_for(role)} command directory"
            )

    errors.extend(check_matrix(root, expected["roles"]))

    found = set(orphans(root))
    allowed = waived(root)
    errors.extend(waiver_hygiene(root))
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
