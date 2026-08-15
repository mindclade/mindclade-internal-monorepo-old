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
from pathlib import Path

_ALLOWED_EMPTY = {"Cargo.lock", "flake.lock", "MODULE.bazel.lock"}
_ALLOWED_EMPTY_PATHS = {"sdk/go/go.sum"}


def repository_root() -> Path:
    return Path(__file__).resolve().parents[2]


def check(root: Path, manifest: Path) -> dict[str, object]:
    paths = [
        line.strip() for line in manifest.read_text(encoding="utf-8").splitlines() if line.strip()
    ]
    duplicates = sorted({path for path in paths if paths.count(path) > 1})
    missing = sorted(path for path in paths if not (root / path).is_file())
    unexpected_empty = sorted(
        path
        for path in paths
        if (root / path).is_file()
        and (root / path).stat().st_size == 0
        and Path(path).name not in _ALLOWED_EMPTY
        and path not in _ALLOWED_EMPTY_PATHS
    )
    unsafe = sorted(path for path in paths if Path(path).is_absolute() or ".." in Path(path).parts)
    return {
        "schema_version": 1,
        "blueprint_path_count": len(paths),
        "materialized_path_count": len(paths) - len(missing),
        "coverage_percent": round(100.0 * (len(paths) - len(missing)) / max(1, len(paths)), 4),
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
        help="Defaults to docs/blueprint/production-monorepo-paths.txt under --root.",
    )
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    manifest = (args.manifest or root / "docs/blueprint/production-monorepo-paths.txt").resolve()
    result = check(root, manifest)
    failed = any(
        result[key]
        for key in ("duplicate_paths", "missing_paths", "unexpected_empty_paths", "unsafe_paths")
    )
    if args.json:
        print(json.dumps(result, indent=2, sort_keys=True))
    else:
        print(
            f"blueprint coverage: {result['materialized_path_count']}/"
            f"{result['blueprint_path_count']} ({result['coverage_percent']:.2f}%)"
        )
        for key in ("duplicate_paths", "missing_paths", "unexpected_empty_paths", "unsafe_paths"):
            values = result[key]
            if values:
                print(f"{key}:", file=sys.stderr)
                for value in values:
                    print(f"  {value}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
