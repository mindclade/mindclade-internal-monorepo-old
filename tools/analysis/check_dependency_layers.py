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

GO_IMPORT = re.compile(r'"(mindclade\.internal/[^\"]+)"')


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
            for dep in GO_IMPORT.findall(p.read_text(errors="replace")):
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
