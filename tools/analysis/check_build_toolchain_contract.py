#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import argparse
import ast
import re
import tomllib
from pathlib import Path

FORBIDDEN = [
    re.compile(r"\bpip\s+install\b"),
    re.compile(r"\bnpm\s+install\b"),
    re.compile(r"\bcargo\s+fetch\b"),
    re.compile(r"curl\b[^\n|]*\|\s*(?:bash|sh)\b"),
    re.compile(r"(?<![\w.-])/usr/bin/(?!env\b)"),
]
SCAN = {".bzl", ".bazel", ".sh", ".py", ".yml", ".yaml", ".nix"}
REQUIRED_REPO_IGNORES = frozenset(
    {
        "**/.mypy_cache",
        "**/.pytest_cache",
        "**/.ruff_cache",
        "**/.terraform",
        "**/__pycache__",
        "**/node_modules",
    }
)
PYTEST_MACRO = "tools/build/pytest.bzl"

# Files that ENFORCE this same contract, and therefore contain the forbidden strings as
# pattern literals rather than as commands.
#
# This file has always exempted itself for that reason — see the `p.resolve() ==
# Path(__file__).resolve()` test below. The exemption was written when this was the only
# implementation; it is not any more. tools/build/nix/checks/no-host-tools.nix enforces the
# same ADR-0002 clause ("CI rejects host-tool leakage") for `nix flake check`, which runs where
# Python does not, and it names /usr/bin and `pip install` for the same reason this file does.
#
# Without the entry the two checks are mutually exclusive: adding the Nix one turns this one
# red, and the only way to a green run is to delete a control.
#
# Scoped to exact paths, not to the directory. tools/build/nix/checks/ holds other checks that
# have no business naming a host path, and skipping all of them to fix one would be trading a
# false positive for a blind spot. What is safe to exempt is a file that DEFINES patterns and
# runs no build action; a new one has to be added here deliberately.
CONTRACT_IMPLEMENTATIONS = frozenset(
    {
        "tools/analysis/verify_cc_toolchain_selection.py",
        "tools/build/nix/checks/no-host-tools.nix",
    }
)


def rust_version_contract(root: Path) -> list[str]:
    errors = []
    cargo = tomllib.loads((root / "Cargo.toml").read_text())
    expected = cargo.get("workspace", {}).get("package", {}).get("rust-version")
    nix = (root / "tools/build/nix/versions.nix").read_text(errors="replace")
    flake = (root / "flake.nix").read_text(errors="replace")
    qualify = (
        (root / "tools/qualification/rust/common.py").read_text(errors="replace")
        if (root / "tools/qualification/rust/common.py").exists()
        else ""
    )
    if not expected:
        errors.append("Cargo workspace rust-version is missing")
        return errors
    if f'rust = "{expected}"' not in nix:
        errors.append("Nix Rust version does not match Cargo rust-version")
    if "rust-overlay" not in flake or "toolchains/rust.nix" not in flake:
        errors.append("flake.nix must construct the pinned Rust toolchain through rust-overlay")
    if expected not in qualify:
        errors.append("Rust qualification expected-version does not match Cargo rust-version")
    return errors


def repository_traversal_contract(root: Path) -> list[str]:
    repo_policy = root / "REPO.bazel"
    if not repo_policy.is_file():
        return ["REPO.bazel is required for globbed generated-directory ignores"]

    text = repo_policy.read_text(errors="replace")
    errors = []
    if "ignore_directories(" not in text:
        errors.append("REPO.bazel must declare ignore_directories()")
    for pattern in sorted(REQUIRED_REPO_IGNORES):
        if f'"{pattern}"' not in text and f"'{pattern}'" not in text:
            errors.append(f"REPO.bazel must ignore generated directory pattern {pattern}")
    return errors


def pytest_init_contract(root: Path) -> list[str]:
    """Keep pytest runfiles source-authoritative.

    rules_python's legacy initializer synthesis can create empty ``__init__.py`` files at
    first-party runfiles paths. In a persistent runfiles tree, one of those paths can still be
    a source symlink from an earlier action, so materializing the empty file can truncate the
    tracked source. The shared pytest macro must therefore default synthesis off and pass that
    decision explicitly to ``py_test``.

    Parse the current Starlark as Python syntax instead of looking for strings: the macro is
    deliberately Python-compatible, and structural checks cannot be satisfied by a comment or
    docstring that merely describes the safe setting.
    """

    path = root / PYTEST_MACRO
    if not path.is_file():
        return [f"{PYTEST_MACRO} is required for pytest package-initializer governance"]

    try:
        module = ast.parse(path.read_text(errors="replace"), filename=PYTEST_MACRO)
    except SyntaxError as error:
        return [f"{PYTEST_MACRO} is not parseable for package-initializer governance: {error.msg}"]

    definitions = [
        node
        for node in module.body
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name == "pytest_test"
    ]
    if len(definitions) != 1:
        return [f"{PYTEST_MACRO} must define exactly one top-level pytest_test macro"]

    macro = definitions[0]
    positional = [*macro.args.posonlyargs, *macro.args.args]
    defaults = {
        argument.arg: default
        for argument, default in zip(
            positional[-len(macro.args.defaults) :], macro.args.defaults, strict=True
        )
    }
    initializer_default = defaults.get("legacy_create_init")
    errors = []
    if not (isinstance(initializer_default, ast.Constant) and initializer_default.value is False):
        errors.append(f"{PYTEST_MACRO}: pytest_test must default legacy_create_init to False")

    py_test_calls = [
        node
        for node in ast.walk(macro)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Name)
        and node.func.id == "py_test"
    ]
    forwards_initializer = False
    if len(py_test_calls) == 1:
        initializer_keywords = [
            keyword.value
            for keyword in py_test_calls[0].keywords
            if keyword.arg == "legacy_create_init"
        ]
        forwards_initializer = (
            len(initializer_keywords) == 1
            and isinstance(initializer_keywords[0], ast.Name)
            and initializer_keywords[0].id == "legacy_create_init"
        )
    if not forwards_initializer:
        errors.append(
            f"{PYTEST_MACRO}: pytest_test must forward legacy_create_init explicitly to py_test"
        )
    return errors


def check(root: Path):
    errors = []
    if (root / "WORKSPACE").exists() or (root / "WORKSPACE.bazel").exists():
        errors.append("legacy WORKSPACE is forbidden; Bzlmod owns Bazel dependencies")
    errors.extend(rust_version_contract(root))
    errors.extend(repository_traversal_contract(root))
    errors.extend(pytest_init_contract(root))
    for p in root.rglob("*"):
        if (
            not p.is_file()
            or p.resolve() == Path(__file__).resolve()
            or p.relative_to(root).as_posix() in CONTRACT_IMPLEMENTATIONS
            # Build and tool output, not source. The list previously stopped at node_modules,
            # which was complete when only JS had a vendor directory. It now misses .venv in
            # particular: `uv sync` writes one, so running this check after the Python lane
            # scanned pip invocations inside site-packages and reported ruff's own __main__.py
            # as a forbidden host package-manager call.
            or any(
                x in p.parts
                for x in (
                    ".git",
                    "node_modules",
                    # Agent worktrees: full COPIES of this repository, so every checker that
                    # rglobs the tree finds a second (third, twelfth) set of every file and
                    # reports each one. They are ephemeral and not part of the source.
                    ".claude",
                    "__pycache__",
                    ".venv",
                    "target",
                    ".ruff_cache",
                    ".pytest_cache",
                    ".mypy_cache",
                )
            )
            # Relative to the repository, not absolute: `root.rglob` yields absolute paths, so
            # p.parts[0] is "/" and this never matched. The convenience symlinks Bazel writes
            # (bazel-out, bazel-bin, bazel-<workspace>) only ever sit at the root.
            or p.relative_to(root).parts[0].startswith("bazel-")
        ):
            continue
        if p.name == "Dockerfile" or p.name.startswith("Dockerfile."):
            text = p.read_text(errors="replace")
            if "MINDCLADE_DEV_ONLY=1" not in text:
                errors.append(
                    f"{p.relative_to(root)}: production Dockerfiles are forbidden; Bazel OCI owns images"
                )
        if p.suffix in SCAN or p.name in {"BUILD", "BUILD.bazel"}:
            text = p.read_text(errors="replace")
            for rx in FORBIDDEN:
                if rx.search(text):
                    errors.append(
                        f"{p.relative_to(root)}: forbidden host/package-manager pattern: {rx.pattern}"
                    )
    return sorted(set(errors))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print(
        "Bazel/Nix ownership check passed"
        if not e
        else f"Bazel/Nix ownership check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
