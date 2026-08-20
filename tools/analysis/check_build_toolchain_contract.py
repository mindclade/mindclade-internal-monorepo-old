#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import argparse
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


def check(root: Path):
    errors = []
    if (root / "WORKSPACE").exists() or (root / "WORKSPACE.bazel").exists():
        errors.append("legacy WORKSPACE is forbidden; Bzlmod owns Bazel dependencies")
    errors.extend(rust_version_contract(root))
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
