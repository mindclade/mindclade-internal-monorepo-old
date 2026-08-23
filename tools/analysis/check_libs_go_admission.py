#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce the libs/go admission allowlist.

Two gaps were closed after an audit of the declared record:

  * `allowed_top_level` was only checked in the "unadmitted directory" direction,
    so an entry for a package that had been removed stayed in the allowlist
    forever and silently pre-admitted a future directory of that name.
  * `forbidden_names` was only applied to top-level directories, so the ban on
    `common`/`shared`/`helpers`/`utils`/... stopped at depth 1 and
    `libs/go/servicekit/helpers` would have been admitted without review. The
    ban is applied to any directory with Go source at or below it, because a
    banned name that holds only subpackages is the same dumping ground.

Neither produced a finding when the checks were added; both are ratchets held
at zero, which is the only point at which a ratchet is cheap to install.
"""

from __future__ import annotations

import argparse
import tomllib
from pathlib import Path

# testdata is invisible to the Go toolchain and vendor is not our source, so a
# fixture named libs/go/<pkg>/testdata/utils must not fail the name ban.
SKIP_DIRECTORIES = {"testdata", "vendor"}


def _named_directories(root: Path) -> list[Path]:
    """Return every directory under libs/go that contains Go source at or below it.

    The name ban applies to a directory that groups Go packages, not only to one
    that holds .go files itself: `libs/go/servicekit/helpers/strings/s.go` makes
    `helpers` a dumping ground just as surely as a file directly inside it.
    """
    base = root / "libs/go"
    return sorted(
        path
        for path in base.rglob("*")
        if path.is_dir()
        and not SKIP_DIRECTORIES & set(path.relative_to(base).parts)
        and any(path.rglob("*.go"))
    )


def check(root: Path) -> list[str]:
    cfg = tomllib.loads((root / "libs/go/ADMISSION.toml").read_text())
    allowed = set(cfg["allowed_top_level"])
    forbidden = set(cfg["forbidden_names"])
    errors = []
    actual = {p.name for p in (root / "libs/go").iterdir() if p.is_dir() and any(p.rglob("*.go"))}
    for x in sorted(actual - allowed):
        errors.append(f"libs/go/{x}: new top-level library is not admitted")
    for x in sorted(allowed - actual):
        errors.append(
            f"libs/go/{x}: admitted in ADMISSION.toml but no such package directory exists; "
            "drop the allowlist entry"
        )
    base = root / "libs/go"
    for path in _named_directories(root):
        if path.name in forbidden:
            errors.append(
                f"libs/go/{path.relative_to(base).as_posix()}: forbidden dumping-ground/domain name"
            )
    return errors


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print("libs/go admission check passed" if not e else "libs/go admission check failed")
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
