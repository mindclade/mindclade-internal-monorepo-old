#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import argparse
from pathlib import Path


def check(root: Path):
    modules = sorted(
        # Agent worktrees are full copies of this repository, so rglob otherwise finds a second
        # go.mod for every real one and reports each as an unexpected module boundary.
        #
        # Match on the path *relative to root*. Matching the absolute path also tested the
        # repository's own location, so a checkout that itself lived under a directory named
        # .claude -- which is exactly where agent worktrees are created -- excluded every go.mod
        # in the tree including the root one, and this check reported "missing root go.mod"
        # against a tree that had not been touched. The sibling walkers in
        # check_license_headers.py and validate_repository.py already relativize first.
        relative.as_posix()
        for relative in (p.relative_to(root) for p in root.rglob("go.mod"))
        if not {".git", ".claude", ".codex-worktrees"} & set(relative.parts)
    )
    allowed = {"go.mod", "sdk/go/go.mod"}
    return [f"unexpected Go module boundary: {m}" for m in modules if m not in allowed] + (
        ["missing root go.mod"] if "go.mod" not in modules else []
    )


def main():
    a = argparse.ArgumentParser()
    a.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    o = a.parse_args()
    e = check(o.repo.resolve())
    [print(x) for x in e]
    print("Go module boundary check passed" if not e else "Go module boundary check failed")
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
