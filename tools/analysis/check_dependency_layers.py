#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce repository dependency direction using simple source-level guards."""

from __future__ import annotations

import argparse
import re
from pathlib import Path

# Must track go.mod's module directive, and the `rules` prefixes below with it. Left on
# `mindclade.internal` by the cutover to go.mindclade.dev, this matched nothing, so every rule
# below forbade nothing and the check reported success on a tree it had not inspected.
GO_IMPORT = re.compile(r'"(go\.mindclade\.dev/[^\"]+)"')

# Comments are stripped first: a quoted module path in prose is not a dependency, and a doc
# comment naming a forbidden package would otherwise be reported as importing it. Safe because
# nothing but the package clause and comments may precede a Go import block.
GO_COMMENT = re.compile(r"/\*.*?\*/|//[^\n]*", re.S)


def check(root: Path):
    e = []
    rules = [
        ("libs/go", "go.mindclade.dev/control/"),
        ("libs/go", "go.mindclade.dev/services/"),
        ("control", "go.mindclade.dev/services/"),
    ]
    for rel, forbidden in rules:
        for p in (root / rel).rglob("*.go"):
            if p.name.endswith("_test.go"):
                continue
            for dep in GO_IMPORT.findall(GO_COMMENT.sub("", p.read_text(errors="replace"))):
                if dep.startswith(forbidden):
                    e.append(f"{p.relative_to(root)}: forbidden dependency {dep}")
    for p in (root / "models").rglob("*.py"):
        if "research." in p.read_text(errors="replace"):
            e.append(f"{p.relative_to(root)}: production model imports research")
    return e


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print("dependency layer check passed" if not e else "dependency layer check failed")
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
