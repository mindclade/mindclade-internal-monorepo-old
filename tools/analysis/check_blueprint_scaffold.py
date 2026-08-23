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
import os
import stat
import sys
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import cast

MANIFEST_RELPATH = "docs/blueprint/production-monorepo-paths.txt"

# Every result key whose value is a list of offending blueprint paths, in report order. Callers
# subtract explicitly permitted non-gating findings from this tuple so a newly added invariant is
# enforced by default instead of being silently omitted from a second allowlist.
DEFECT_KEYS = ("duplicate_paths", "missing_paths", "unexpected_empty_paths", "unsafe_paths")

_ALLOWED_EMPTY = {"Cargo.lock", "flake.lock", "MODULE.bazel.lock"}
_ALLOWED_EMPTY_PATHS = {"sdk/go/go.sum"}


@dataclass(frozen=True)
class _PathInspection:
    path: Path
    file_stat: os.stat_result | None
    error: str | None = None


def repository_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _result(
    *,
    manifest_sha256: str = "",
    manifest_errors: list[str] | None = None,
    paths: list[str] | None = None,
    duplicates: list[str] | None = None,
    missing: list[str] | None = None,
    unexpected_empty: list[str] | None = None,
    unsafe: list[str] | None = None,
    materialized: int = 0,
) -> dict[str, object]:
    listed_paths = paths or []
    return {
        "schema_version": 1,
        "blueprint_path_count": len(listed_paths),
        "materialized_path_count": materialized,
        "coverage_percent": round(100.0 * materialized / max(1, len(listed_paths)), 4),
        "manifest_sha256": manifest_sha256,
        "manifest_errors": manifest_errors or [],
        "duplicate_paths": duplicates or [],
        "missing_paths": missing or [],
        "unexpected_empty_paths": unexpected_empty or [],
        "unsafe_paths": unsafe or [],
    }


def _walk_without_symlinks(canonical_root: Path, candidate: Path) -> _PathInspection:
    try:
        relative = candidate.relative_to(canonical_root)
    except ValueError:
        return _PathInspection(candidate, None, "path resolves outside the repository root")
    if relative.is_absolute() or ".." in relative.parts:
        return _PathInspection(candidate, None, "path escapes the repository root")

    current = canonical_root
    components = relative.parts
    if not components:
        try:
            return _PathInspection(current, current.lstat())
        except OSError:
            return _PathInspection(current, None, "path could not be inspected")

    for index, component in enumerate(components):
        current /= component
        try:
            file_stat = current.lstat()
        except FileNotFoundError:
            return _PathInspection(candidate, None)
        except OSError:
            return _PathInspection(candidate, None, "path could not be inspected")
        if stat.S_ISLNK(file_stat.st_mode):
            return _PathInspection(candidate, None, "path contains a symbolic link")
        if index < len(components) - 1 and not stat.S_ISDIR(file_stat.st_mode):
            return _PathInspection(candidate, None)

    return _PathInspection(candidate, file_stat)


def _same_file(left: os.stat_result, right: os.stat_result) -> bool:
    return (
        left.st_dev,
        left.st_ino,
        left.st_mode,
        left.st_size,
        left.st_mtime_ns,
        left.st_ctime_ns,
    ) == (
        right.st_dev,
        right.st_ino,
        right.st_mode,
        right.st_size,
        right.st_mtime_ns,
        right.st_ctime_ns,
    )


def _strict_contained_path(canonical_root: Path, relative: Path) -> _PathInspection:
    """Inspect a repository-relative path without following any symbolic link.

    The double walk catches replacement races around canonical resolution. Callers consume the
    returned lstat metadata directly, so no later `is_file` or following `stat` can escape the
    repository after this check.
    """
    if relative.is_absolute() or ".." in relative.parts:
        return _PathInspection(canonical_root / relative, None, "path escapes the repository root")
    candidate = canonical_root / relative
    before = _walk_without_symlinks(canonical_root, candidate)
    if before.error is not None or before.file_stat is None:
        return before

    try:
        resolved = candidate.resolve(strict=True)
        resolved.relative_to(canonical_root)
    except FileNotFoundError:
        return _PathInspection(candidate, None)
    except (OSError, RuntimeError, ValueError):
        return _PathInspection(candidate, None, "path resolves outside the repository root")

    after = _walk_without_symlinks(canonical_root, candidate)
    if after.error is not None or after.file_stat is None:
        return after
    if not _same_file(before.file_stat, after.file_stat):
        return _PathInspection(candidate, None, "path changed during inspection")
    return after


def _read_strict_file(canonical_root: Path, relative: Path) -> tuple[bytes | None, str | None]:
    inspection = _strict_contained_path(canonical_root, relative)
    if inspection.error is not None:
        return None, inspection.error
    if inspection.file_stat is None:
        return None, "path is missing"
    if not stat.S_ISREG(inspection.file_stat.st_mode):
        return None, "path is not a regular file"

    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        with os.fdopen(os.open(inspection.path, flags), "rb") as handle:
            opened_before = os.fstat(handle.fileno())
            contents = handle.read()
            opened_after = os.fstat(handle.fileno())
    except OSError:
        return None, "path could not be read safely"
    if not _same_file(inspection.file_stat, opened_before) or not _same_file(
        opened_before, opened_after
    ):
        return None, "path changed while it was read"

    verified = _strict_contained_path(canonical_root, relative)
    if verified.error is not None:
        return None, verified.error
    if verified.file_stat is None or not _same_file(opened_after, verified.file_stat):
        return None, "path changed while it was read"
    return contents, None


def _canonical_root(root: Path) -> tuple[Path | None, str | None]:
    try:
        canonical = Path(os.path.abspath(root)).resolve(strict=True)
        if not stat.S_ISDIR(canonical.lstat().st_mode):
            return None, "repository root is not a directory"
    except (OSError, RuntimeError):
        return None, "repository root could not be inspected"
    return canonical, None


def _manifest_relative_path(
    root: Path, canonical_root: Path, manifest: Path
) -> tuple[Path | None, str]:
    lexical_root = Path(os.path.abspath(root))
    lexical_manifest = Path(os.path.abspath(manifest))
    for base in dict.fromkeys((lexical_root, canonical_root)):
        try:
            relative = lexical_manifest.relative_to(base)
        except ValueError:
            continue
        if not relative.is_absolute() and ".." not in relative.parts:
            return relative, relative.as_posix()
    return None, str(lexical_manifest)


def has_failures(result: dict[str, object], *, include_missing: bool = True) -> bool:
    if cast("list[str]", result["manifest_errors"]):
        return True
    return any(result[key] for key in DEFECT_KEYS if include_missing or key != "missing_paths")


def check(root: Path, manifest: Path) -> dict[str, object]:
    canonical_root, root_error = _canonical_root(root)
    if canonical_root is None:
        return _result(manifest_errors=[f"blueprint {root_error}: {root}"])

    manifest_relative, manifest_display = _manifest_relative_path(root, canonical_root, manifest)
    if manifest_relative is None:
        return _result(
            manifest_errors=[
                f"blueprint manifest is outside the repository root: {manifest_display}"
            ]
        )
    manifest_bytes, manifest_error = _read_strict_file(canonical_root, manifest_relative)
    if manifest_bytes is None:
        if manifest_error == "path is missing":
            message = f"blueprint manifest is missing: {manifest_display}"
        else:
            message = f"blueprint manifest is unsafe: {manifest_display} ({manifest_error})"
        return _result(manifest_errors=[message])

    manifest_sha256 = hashlib.sha256(manifest_bytes).hexdigest()
    try:
        manifest_text = manifest_bytes.decode("utf-8")
    except UnicodeDecodeError:
        return _result(
            manifest_sha256=manifest_sha256,
            manifest_errors=[f"blueprint manifest is not valid UTF-8: {manifest_display}"],
        )
    paths = [line.strip() for line in manifest_text.splitlines() if line.strip()]
    if not paths:
        return _result(
            manifest_sha256=manifest_sha256,
            manifest_errors=[f"blueprint manifest lists no paths: {manifest_display}"],
        )

    # One pass. `paths.count(path)` inside the comprehension rescanned the whole list per entry,
    # which is ~20M comparisons for a manifest this size.
    duplicates = sorted(path for path, n in Counter(paths).items() if n > 1)
    unsafe = sorted(path for path in paths if Path(path).is_absolute() or ".." in Path(path).parts)

    # Lexical rejection happens before any per-entry filesystem probe. In particular, pathlib
    # discards `root` when the right operand is absolute, so probing first can escape the checkout.
    rejected = set(unsafe)
    missing: list[str] = []
    unexpected_empty: list[str] = []
    materialized = 0
    for path in paths:
        if path in rejected:
            continue
        inspection = _strict_contained_path(canonical_root, Path(path))
        if inspection.error is not None:
            unsafe.append(path)
            continue
        if inspection.file_stat is None or not stat.S_ISREG(inspection.file_stat.st_mode):
            missing.append(path)
            continue
        materialized += 1
        if (
            inspection.file_stat.st_size == 0
            and Path(path).name not in _ALLOWED_EMPTY
            and path not in _ALLOWED_EMPTY_PATHS
        ):
            unexpected_empty.append(path)

    return _result(
        manifest_sha256=manifest_sha256,
        paths=paths,
        duplicates=duplicates,
        missing=sorted(missing),
        unexpected_empty=sorted(unexpected_empty),
        unsafe=sorted(set(unsafe)),
        materialized=materialized,
    )


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
    root = args.root
    manifest = args.manifest or root / MANIFEST_RELPATH
    result = check(root, manifest)
    failed = has_failures(result)
    if args.json:
        print(json.dumps(result, indent=2, sort_keys=True))
    else:
        print(
            f"blueprint coverage: {result['materialized_path_count']}/"
            f"{result['blueprint_path_count']} ({result['coverage_percent']:.2f}%)"
        )
        for error in cast("list[str]", result["manifest_errors"]):
            print(error, file=sys.stderr)
        for key in DEFECT_KEYS:
            values = cast("list[str]", result[key])
            if values:
                print(f"{key}:", file=sys.stderr)
                for value in values:
                    print(f"  {value}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
