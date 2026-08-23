#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce the shape of Go control-plane command packages.

Replaces tests/integration/go_foundation/consumption_test.py, which asserted
that every command wires bootstrap.UnconfiguredFactory -- true only while no
role had a provider factory, so it turned materializing one into a test
failure. It also re-derived the consumption matrix by grepping package-name
strings out of consumption.go; check_foundation_consumption.py now derives that
from the import graph instead.

What survives here is the part that is still an invariant: a command owns no
lifecycle of its own -- plus one the old test never covered: a command that
links the PostgreSQL pool must also link a driver.

That second rule exists because the failure it catches is invisible. Seven
commands opened a pool while importing no driver, so every one of them would
have died at startup on "database_driver_not_linked". Their factory tests all
passed, because a _test.go file imported lib/pq and put the driver in the test
binary rather than the real one. A guard that reads the production import graph
is the only thing that can tell those two apart.
"""

from __future__ import annotations

import argparse
import ast
import re
import sys
from pathlib import Path

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import go_import_graph as graphs  # noqa: E402  (path is established immediately above)

COMMAND_ROOT = "services/control_plane/cmd"
PROFILE_PATH = "services/control_plane/internal/bootstrap/profile.go"
ROLE_CONST_RE = re.compile(r'(?m)^\s*(?:const\s+)?(Role[A-Za-z]+)\s+Role\s*=\s*"([a-z0-9-]+)"')

# Importing this package is what opens a *sql.DB, so it is the precise
# condition under which a driver must be registered.
SQL_POOL = "libs/go/storage/sql/postgres"

# database/sql drivers this repository may link. A driver is registered by an
# import side effect, so only the main package may choose one; adding an entry
# here is the reviewed step that admits a new driver.
DRIVERS = (
    "github.com/lib/pq",
    "github.com/jackc/pgx/v5/stdlib",
    "github.com/jackc/pgx/v4/stdlib",
)

FORBIDDEN = {
    "servicekit.New(": "command bypasses servicekit/production bootstrap",
    "servicekit.NewAssembly(": "command assembles its own service",
    "signal.Notify": "command takes signal ownership from servicekit",
}


def _named_calls(tree: ast.AST, name: str) -> list[ast.Call]:
    return [
        node
        for node in ast.walk(tree)
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == name
    ]


def _loaded_symbols(tree: ast.AST, label: str) -> set[str]:
    symbols: set[str] = set()
    for call in _named_calls(tree, "load"):
        if (
            not call.args
            or not isinstance(call.args[0], ast.Constant)
            or call.args[0].value != label
        ):
            continue
        symbols.update(
            argument.value
            for argument in call.args[1:]
            if isinstance(argument, ast.Constant) and isinstance(argument.value, str)
        )
        symbols.update(
            keyword.arg
            for keyword in call.keywords
            if keyword.arg is not None
            and isinstance(keyword.value, ast.Constant)
            and keyword.value.value == "go_binary"
        )
    return symbols


def _dependency_labels(tree: ast.AST) -> set[str]:
    labels: set[str] = set()
    for rule in ("go_binary", "go_library"):
        for call in _named_calls(tree, rule):
            for keyword in call.keywords:
                if keyword.arg != "deps":
                    continue
                labels.update(
                    node.value
                    for node in ast.walk(keyword.value)
                    if isinstance(node, ast.Constant) and isinstance(node.value, str)
                )
    return labels


def build_contract_errors(source: str, label: str) -> list[str]:
    try:
        tree = ast.parse(source, filename=label)
    except SyntaxError as error:
        return [f"{label}: BUILD syntax is not structurally parseable: {error.msg}"]
    errors: list[str] = []
    if "go_binary" not in _loaded_symbols(tree, "@rules_go//go:def.bzl"):
        errors.append(f'{label}: must load go_binary from "@rules_go//go:def.bzl"')
    if not _named_calls(tree, "go_binary"):
        errors.append(f"{label}: must declare a go_binary rule")
    bootstrap = "//services/control_plane/internal/bootstrap"
    if bootstrap not in _dependency_labels(tree):
        errors.append(f"{label}: must declare dependency {bootstrap}")
    return errors


def check(root: Path) -> list[str]:
    command_root = root / COMMAND_ROOT
    if not command_root.is_dir():
        return [f"{COMMAND_ROOT}: missing command root"]

    roles = dict(ROLE_CONST_RE.findall((root / PROFILE_PATH).read_text(encoding="utf-8")))
    by_value = {value: name for name, value in roles.items()}

    module = graphs.module_path(root)
    graph = graphs.import_graph(root)
    pool_package = f"{module}/{SQL_POOL}"

    errors: list[str] = []
    for directory in sorted(path for path in command_root.iterdir() if path.is_dir()):
        command = directory.name
        role = by_value.get(command.replace("_", "-"))
        if role is None:
            errors.append(f"{COMMAND_ROOT}/{command}: no Role constant declares this command")
            continue

        main = directory / "main.go"
        if not main.is_file():
            errors.append(f"{COMMAND_ROOT}/{command}: missing main.go")
            continue
        source = main.read_text(encoding="utf-8")
        for token in (
            '"go.mindclade.dev/services/control_plane/internal/bootstrap"',
            "bootstrap.Main(",
            f"bootstrap.{role}",
        ):
            if token not in source:
                errors.append(f"{COMMAND_ROOT}/{command}/main.go: must contain {token}")
        for token, message in FORBIDDEN.items():
            if token in source:
                errors.append(f"{COMMAND_ROOT}/{command}/main.go: {message}")

        # The pool is reached transitively, through the role's provider
        # factory, so the command's own imports cannot answer this.
        reachable = graphs.transitive_imports(graph, [f"{module}/{COMMAND_ROOT}/{command}"])
        if pool_package in reachable:
            imports = graphs.file_imports(source)
            if not any(driver in imports for driver in DRIVERS):
                errors.append(
                    f"{COMMAND_ROOT}/{command}/main.go: links {SQL_POOL} but registers no "
                    f"database/sql driver; add a blank import of one of {', '.join(DRIVERS)}"
                )
        elif any(driver in graphs.file_imports(source) for driver in DRIVERS):
            errors.append(
                f"{COMMAND_ROOT}/{command}/main.go: registers a database/sql driver but never "
                f"links {SQL_POOL}; drop the import"
            )

        build = directory / "BUILD.bazel"
        if not build.is_file():
            errors.append(f"{COMMAND_ROOT}/{command}: missing BUILD.bazel")
            continue
        errors.extend(
            build_contract_errors(
                build.read_text(encoding="utf-8"),
                f"{COMMAND_ROOT}/{command}/BUILD.bazel",
            )
        )
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    errors = check(args.repo.resolve())
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f"control-plane command check failed with {len(errors)} finding(s)", file=sys.stderr)
        return 1
    print("control-plane command check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
