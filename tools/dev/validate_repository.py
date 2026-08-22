#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Repository-level structural and design validation.

This command intentionally validates what can be established without live cloud
providers. Provider, Rust runtime, GPU, Bazel remote execution, and release
qualification remain separate evidence lanes.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tomllib
from pathlib import Path

sys.dont_write_bytecode = True

_LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")

# Vendored and tool-output trees. This is the set proven in
# tools/analysis/check_build_toolchain_contract.py, and it is here for the same reason: these
# directories hold third-party or regenerated content that the repository does not own, so
# reporting on them is noise at best and wrong at worst.
#
# .venv is the one that actually bit. Every walk below stopped at node_modules/.git, which was
# complete when only JS vendored into the tree. `uv sync` then wrote a .venv, and the hygiene
# walk began reporting ~180 __pycache__ directories under site-packages -- burying the ~35
# genuine in-repo reports -- while structured_files parsed every JSON/TOML/YAML shipped by
# torch and numpy on the way past.
#
# .claude and .codex-worktrees hold agent worktrees -- full checkouts of this repository nested
# inside them. Without these exclusions every finding is reported once per live worktree, and a
# transient agent checkout is not something the repository should be validating.
_VENDORED = frozenset(
    {
        ".claude",
        ".codex-worktrees",
        ".git",
        ".mypy_cache",
        ".pytest_cache",
        ".ruff_cache",
        ".venv",
        "__pycache__",
        "node_modules",
        "target",
    }
)


def _walk(root: Path):
    """Yield every file under root, pruning vendored trees instead of filtering them.

    os.walk rather than Path.rglob: rglob has no way to stop descending, so it still visits
    every file in .venv even when the caller discards them. Pruning dirnames in place is what
    makes this cheap -- and cheap matters, because a full site-packages traversal is what made
    this validator slow enough to look hung.
    """
    root = root.resolve()
    for dirpath, dirnames, filenames in os.walk(root):
        here = Path(dirpath)
        # Bazel writes bazel-out/bazel-bin/bazel-<workspace> convenience SYMLINKS at the
        # repository root. Relative to root, so parts[0] is the first repo-level component --
        # against an absolute path parts[0] is "/" and this never matches.
        relative = here.relative_to(root)
        if relative.parts and relative.parts[0].startswith("bazel-"):
            dirnames[:] = []
            continue
        dirnames[:] = sorted(d for d in dirnames if d not in _VENDORED)
        for name in sorted(filenames):
            yield here / name


def _walk_dirs(root: Path):
    """Yield every directory under root, pruning vendored trees but still reporting them.

    Distinct from _walk because hygiene has to *name* a transient directory it finds. Pruning
    happens after the yield so an in-repo __pycache__ is still reported -- it is simply not
    descended into.
    """
    root = root.resolve()
    for dirpath, dirnames, _ in os.walk(root):
        here = Path(dirpath)
        relative = here.relative_to(root)
        if relative.parts and relative.parts[0].startswith("bazel-"):
            dirnames[:] = []
            continue
        for name in sorted(dirnames):
            yield here / name
        dirnames[:] = sorted(d for d in dirnames if d not in _VENDORED)


def structured_files(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        import yaml  # type: ignore
    except Exception:
        yaml = None
    # One traversal for all three formats. Three rglob passes re-walked the tree per suffix,
    # which is wasted work once the walk is pruned rather than filtered.
    for p in _walk(root):
        suffix = p.suffix
        if suffix == ".json":
            try:
                json.loads(p.read_text())
            except Exception as exc:
                errors.append(f"JSON parse failed {p.relative_to(root)}: {exc}")
        elif suffix == ".toml":
            try:
                tomllib.loads(p.read_text())
            except Exception as exc:
                errors.append(f"TOML parse failed {p.relative_to(root)}: {exc}")
        elif suffix in {".yaml", ".yml"} and yaml is not None:
            try:
                for _ in yaml.safe_load_all(p.read_text()):
                    pass
            except Exception as exc:
                errors.append(f"YAML parse failed {p.relative_to(root)}: {exc}")
    return errors


def markdown_links(root: Path) -> list[str]:
    errors: list[str] = []
    for p in _walk(root):
        if p.suffix != ".md":
            continue
        text = p.read_text(errors="replace")
        for raw in _LINK.findall(text):
            target = raw.strip().split()[0].strip("<>")
            if not target or target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            target = target.split("#", 1)[0]
            if not target:
                continue
            resolved = (p.parent / target).resolve()
            try:
                resolved.relative_to(root.resolve())
            except ValueError:
                errors.append(f"Markdown link escapes repository: {p.relative_to(root)} -> {raw}")
                continue
            if not resolved.exists():
                errors.append(f"broken local Markdown link: {p.relative_to(root)} -> {raw}")
    return errors


def hygiene(root: Path) -> list[str]:
    errors = []
    transient_dirs = {"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache"}
    # _walk_dirs still reports an in-repo __pycache__; it just refuses to descend into one, and
    # never enters .venv/node_modules/target at all. `make clean-python` removes what this finds.
    for d in _walk_dirs(root):
        if d.name in transient_dirs:
            errors.append(f"generated cache directory present: {d.relative_to(root)}")
    for p in _walk(root):
        if p.name in {".DS_Store", ".coverage"}:
            errors.append(f"transient file present: {p.relative_to(root)}")
    return errors


def run(cmd: list[str], root: Path) -> tuple[int, str]:
    env = os.environ.copy()
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    proc = subprocess.run(
        cmd, cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT
    )
    return proc.returncode, proc.stdout


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    ap.add_argument("--go", choices=("none", "offline", "connected"), default="none")
    args = ap.parse_args()
    root = args.repo.resolve()
    errors = []

    for name, fn in (
        ("hygiene", hygiene),
        ("structured files", structured_files),
        ("Markdown links", markdown_links),
    ):
        found = fn(root)
        if found:
            errors.extend(f"{name}: {x}" for x in found)
        else:
            print(f"PASS {name}")

    rc, out = run([sys.executable, "tools/analysis/run_architecture_checks.py"], root)
    print(out, end="")
    if rc:
        errors.append("architecture policy failed")

    if args.go != "none":
        rc, out = run(["tools/qualification/go/validate.sh", args.go], root)
        print(out, end="")
        if rc:
            errors.append(f"Go {args.go} qualification failed")

    if errors:
        for e in errors:
            print(f"ERROR {e}")
        print(f"repository validation failed: {len(errors)} issue(s)")
        return 1
    print("repository validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
