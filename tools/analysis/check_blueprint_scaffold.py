#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Verify that every explicit target-state blueprint path is materialized."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections import Counter
from pathlib import Path

MANIFEST_RELPATH = "docs/blueprint/production-monorepo-paths.txt"

# Every result key whose value is a list of offending blueprint paths, in report order. Callers
# subtract explicitly permitted non-gating findings from this tuple so a newly added invariant is
# enforced by default instead of being silently omitted from a second allowlist.
DEFECT_KEYS = ("duplicate_paths", "missing_paths", "unexpected_empty_paths", "unsafe_paths")

_ALLOWED_EMPTY = {"Cargo.lock", "flake.lock", "MODULE.bazel.lock"}
_ALLOWED_EMPTY_PATHS = {"sdk/go/go.sum"}


def repository_root() -> Path:
    return Path(__file__).resolve().parents[2]


def check(root: Path, manifest: Path) -> dict[str, object]:
    paths = [
        line.strip() for line in manifest.read_text(encoding="utf-8").splitlines() if line.strip()
    ]
    # One pass. `paths.count(path)` inside the comprehension rescanned the whole list per entry,
    # which is ~20M comparisons for a manifest this size.
    duplicates = sorted(path for path, n in Counter(paths).items() if n > 1)
    unsafe = sorted(path for path in paths if Path(path).is_absolute() or ".." in Path(path).parts)

    # Never touch the filesystem for an unsafe manifest entry. In particular, pathlib discards
    # `root` when the right operand is absolute, so checking `(root / path).is_file()` first can
    # stat a file outside the checkout and incorrectly count it as materialized.
    rejected = set(unsafe)
    safe_paths = [path for path in paths if path not in rejected]
    missing = sorted(path for path in safe_paths if not (root / path).is_file())
    unexpected_empty = sorted(
        path
        for path in safe_paths
        if (root / path).is_file()
        and (root / path).stat().st_size == 0
        and Path(path).name not in _ALLOWED_EMPTY
        and path not in _ALLOWED_EMPTY_PATHS
    )
    materialized = len(safe_paths) - len(missing)
    return {
        "schema_version": 1,
        "blueprint_path_count": len(paths),
        "materialized_path_count": materialized,
        "coverage_percent": round(100.0 * materialized / max(1, len(paths)), 4),
        "manifest_sha256": hashlib.sha256(manifest.read_bytes()).hexdigest(),
        "duplicate_paths": duplicates,
        "missing_paths": missing,
        "unexpected_empty_paths": unexpected_empty,
        "unsafe_paths": unsafe,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=repository_root())
    parser.add_argument(
        "--manifest",
        type=Path,
        default=None,
        help=f"Defaults to {MANIFEST_RELPATH} under --root.",
    )
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    manifest = (args.manifest or root / MANIFEST_RELPATH).resolve()
    result = check(root, manifest)
    failed = any(result[key] for key in DEFECT_KEYS)
    if args.json:
        print(json.dumps(result, indent=2, sort_keys=True))
    else:
        print(
            f"blueprint coverage: {result['materialized_path_count']}/"
            f"{result['blueprint_path_count']} ({result['coverage_percent']:.2f}%)"
        )
        for key in DEFECT_KEYS:
            values = result[key]
            if values:
                print(f"{key}:", file=sys.stderr)
                for value in values:
                    print(f"  {value}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
