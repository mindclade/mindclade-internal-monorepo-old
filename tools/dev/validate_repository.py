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


def structured_files(root: Path) -> list[str]:
    errors: list[str] = []
    for p in root.rglob("*.json"):
        if any(x in p.parts for x in ("node_modules", ".git")):
            continue
        try:
            json.loads(p.read_text())
        except Exception as exc:
            errors.append(f"JSON parse failed {p.relative_to(root)}: {exc}")
    for p in root.rglob("*.toml"):
        if any(x in p.parts for x in ("node_modules", ".git")):
            continue
        try:
            tomllib.loads(p.read_text())
        except Exception as exc:
            errors.append(f"TOML parse failed {p.relative_to(root)}: {exc}")
    try:
        import yaml  # type: ignore
    except Exception:
        yaml = None
    if yaml is not None:
        for pattern in ("*.yaml", "*.yml"):
            for p in root.rglob(pattern):
                if any(x in p.parts for x in ("node_modules", ".git")):
                    continue
                try:
                    for _ in yaml.safe_load_all(p.read_text()):
                        pass
                except Exception as exc:
                    errors.append(f"YAML parse failed {p.relative_to(root)}: {exc}")
    return errors


def markdown_links(root: Path) -> list[str]:
    errors: list[str] = []
    for p in root.rglob("*.md"):
        if any(x in p.parts for x in ("node_modules", ".git")):
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
    for d in root.rglob("*"):
        if d.is_dir() and d.name in transient_dirs:
            errors.append(f"generated cache directory present: {d.relative_to(root)}")
    for p in root.rglob("*"):
        if p.is_file() and p.name in {".DS_Store", ".coverage"}:
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
