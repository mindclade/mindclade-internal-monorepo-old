#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import argparse
from pathlib import Path


def check(root: Path):
    modules = sorted(
        # `.claude` and `.codex-worktrees` alongside `.git`: agent worktrees are full copies of
        # this repository, so rglob finds a second go.mod for every real one and reports each as
        # an unexpected module boundary. They are ephemeral working directories, not source.
        p.relative_to(root).as_posix()
        for p in root.rglob("go.mod")
        if not {".git", ".claude", ".codex-worktrees"} & set(p.parts)
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
