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


@dataclass(frozen=True)
class _DirectoryTraversal:
    directory_fd: int | None
    leaf_name: str | None
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


def _same_directory(left: os.stat_result, right: os.stat_result) -> bool:
    return (
        stat.S_ISDIR(left.st_mode)
        and stat.S_ISDIR(right.st_mode)
        and (left.st_dev, left.st_ino) == (right.st_dev, right.st_ino)
    )


def _required_open_flags(*names: str) -> tuple[int | None, str | None]:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0)
    for name in names:
        value = getattr(os, name, None)
        if value is None:
            return None, f"platform does not support safe path traversal ({name})"
        flags |= cast("int", value)
    return flags, None


def _lexical_error(relative: Path) -> str | None:
    try:
        raw_path = os.fspath(relative)
        if "\x00" in raw_path:
            return "path contains an invalid NUL character"
        if relative.is_absolute() or ".." in relative.parts:
            return "path escapes the repository root"
    except (OSError, TypeError, ValueError):
        return "path is not a valid repository-relative path"
    return None


def _close_fd(file_descriptor: int) -> None:
    os.close(file_descriptor)


def _descend_directories(
    starting_fd: int, components: tuple[str, ...], directory_flags: int
) -> tuple[int | None, str | None]:
    """Take ownership of starting_fd and open each directory without following symlinks."""
    current_fd = starting_fd
    for component in components:
        try:
            entry_stat = os.stat(component, dir_fd=current_fd, follow_symlinks=False)
        except FileNotFoundError:
            _close_fd(current_fd)
            return None, "path is missing"
        except (OSError, ValueError):
            _close_fd(current_fd)
            return None, "path could not be inspected safely"
        if stat.S_ISLNK(entry_stat.st_mode):
            _close_fd(current_fd)
            return None, "path contains a symbolic link"
        if not stat.S_ISDIR(entry_stat.st_mode):
            _close_fd(current_fd)
            return None, "path is missing"

        try:
            next_fd = os.open(component, directory_flags, dir_fd=current_fd)
        except (OSError, ValueError):
            _close_fd(current_fd)
            return None, "path changed or could not be inspected safely"
        try:
            opened_stat = os.fstat(next_fd)
        except (OSError, ValueError):
            _close_fd(next_fd)
            _close_fd(current_fd)
            return None, "path changed or could not be inspected safely"
        if not _same_directory(entry_stat, opened_stat):
            _close_fd(next_fd)
            _close_fd(current_fd)
            return None, "path changed during inspection"
        _close_fd(current_fd)
        current_fd = next_fd
    return current_fd, None


def _open_repository_root(canonical_root: Path) -> tuple[int | None, str | None]:
    directory_flags, flag_error = _required_open_flags("O_DIRECTORY", "O_NOFOLLOW")
    if directory_flags is None:
        return None, flag_error
    if not canonical_root.is_absolute() or not canonical_root.anchor:
        return None, "repository root is not absolute"

    try:
        root_fd = os.open(canonical_root.anchor, directory_flags)
    except (OSError, ValueError):
        return None, "repository root could not be opened safely"
    try:
        root_stat = os.fstat(root_fd)
    except (OSError, ValueError):
        _close_fd(root_fd)
        return None, "repository root could not be opened safely"
    if not stat.S_ISDIR(root_stat.st_mode):
        _close_fd(root_fd)
        return None, "repository root is not a directory"

    components = tuple(
        component for component in canonical_root.parts if component != canonical_root.anchor
    )
    return _descend_directories(root_fd, components, directory_flags)


def _open_parent_directory(root_fd: int, relative: Path) -> _DirectoryTraversal:
    lexical_error = _lexical_error(relative)
    if lexical_error is not None:
        return _DirectoryTraversal(None, None, lexical_error)

    components = relative.parts
    try:
        starting_fd = os.dup(root_fd)
    except OSError:
        return _DirectoryTraversal(None, None, "repository root descriptor could not be duplicated")
    if not components:
        return _DirectoryTraversal(starting_fd, None)

    directory_flags, flag_error = _required_open_flags("O_DIRECTORY", "O_NOFOLLOW")
    if directory_flags is None:
        _close_fd(starting_fd)
        return _DirectoryTraversal(None, None, flag_error)
    parent_fd, traversal_error = _descend_directories(starting_fd, components[:-1], directory_flags)
    if parent_fd is None:
        return _DirectoryTraversal(None, None, traversal_error)
    return _DirectoryTraversal(parent_fd, components[-1])


def _strict_contained_path(canonical_root: Path, root_fd: int, relative: Path) -> _PathInspection:
    """Inspect a repository-relative path through directory descriptors only."""
    candidate = canonical_root / relative
    traversal = _open_parent_directory(root_fd, relative)
    if traversal.error is not None:
        if traversal.error == "path is missing":
            return _PathInspection(candidate, None)
        return _PathInspection(candidate, None, traversal.error)
    if traversal.directory_fd is None:
        return _PathInspection(candidate, None, "path could not be inspected safely")
    try:
        if traversal.leaf_name is None:
            file_stat = os.fstat(traversal.directory_fd)
        else:
            file_stat = os.stat(
                traversal.leaf_name,
                dir_fd=traversal.directory_fd,
                follow_symlinks=False,
            )
    except FileNotFoundError:
        return _PathInspection(candidate, None)
    except (OSError, ValueError):
        return _PathInspection(candidate, None, "path could not be inspected safely")
    finally:
        _close_fd(traversal.directory_fd)
    if stat.S_ISLNK(file_stat.st_mode):
        return _PathInspection(candidate, None, "path contains a symbolic link")
    return _PathInspection(candidate, file_stat)


def _read_strict_file(root_fd: int, relative: Path) -> tuple[bytes | None, str | None]:
    traversal = _open_parent_directory(root_fd, relative)
    if traversal.error is not None:
        return None, traversal.error
    if traversal.directory_fd is None:
        return None, "path could not be inspected safely"
    if traversal.leaf_name is None:
        _close_fd(traversal.directory_fd)
        return None, "path is not a regular file"

    opened_fd: int | None = None
    try:
        try:
            inspected_stat = os.stat(
                traversal.leaf_name,
                dir_fd=traversal.directory_fd,
                follow_symlinks=False,
            )
        except FileNotFoundError:
            return None, "path is missing"
        except (OSError, ValueError):
            return None, "path could not be inspected safely"
        if stat.S_ISLNK(inspected_stat.st_mode):
            return None, "path contains a symbolic link"
        if not stat.S_ISREG(inspected_stat.st_mode):
            return None, "path is not a regular file"

        file_flags, flag_error = _required_open_flags("O_NOFOLLOW", "O_NONBLOCK")
        if file_flags is None:
            return None, flag_error
        try:
            opened_fd = os.open(
                traversal.leaf_name,
                file_flags,
                dir_fd=traversal.directory_fd,
            )
            opened_before = os.fstat(opened_fd)
        except (OSError, ValueError):
            return None, "path changed or could not be opened safely"

        if not stat.S_ISREG(opened_before.st_mode) or not _same_file(inspected_stat, opened_before):
            return None, "path changed before it was read"

        try:
            with os.fdopen(opened_fd, "rb") as handle:
                opened_fd = None
                contents = handle.read()
                opened_after = os.fstat(handle.fileno())
        except (OSError, ValueError):
            return None, "path could not be read safely"
        if not _same_file(opened_before, opened_after):
            return None, "path changed while it was read"

        try:
            verified_stat = os.stat(
                traversal.leaf_name,
                dir_fd=traversal.directory_fd,
                follow_symlinks=False,
            )
        except (OSError, ValueError):
            return None, "path changed while it was read"
        if stat.S_ISLNK(verified_stat.st_mode) or not _same_file(opened_after, verified_stat):
            return None, "path changed while it was read"
        return contents, None
    finally:
        if opened_fd is not None:
            _close_fd(opened_fd)
        _close_fd(traversal.directory_fd)


def _canonical_root(root: Path) -> tuple[Path | None, str | None]:
    try:
        canonical = Path(os.path.abspath(root)).resolve(strict=True)
        if not stat.S_ISDIR(canonical.lstat().st_mode):
            return None, "repository root is not a directory"
    except (OSError, RuntimeError, ValueError):
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

    root_fd, root_open_error = _open_repository_root(canonical_root)
    if root_fd is None:
        return _result(manifest_errors=[f"blueprint {root_open_error}: {root}"])

    try:
        manifest_relative, manifest_display = _manifest_relative_path(
            root, canonical_root, manifest
        )
        if manifest_relative is None:
            return _result(
                manifest_errors=[
                    f"blueprint manifest is outside the repository root: {manifest_display}"
                ]
            )
        manifest_bytes, manifest_error = _read_strict_file(root_fd, manifest_relative)
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
        duplicates = sorted(path for path, count in Counter(paths).items() if count > 1)
        unsafe = sorted(path for path in paths if _lexical_error(Path(path)) is not None)

        # Lexical rejection happens before any per-entry filesystem probe. In particular, pathlib
        # discards `root` when the right operand is absolute, so probing first can escape the checkout.
        rejected = set(unsafe)
        missing: list[str] = []
        unexpected_empty: list[str] = []
        materialized = 0
        for path in paths:
            if path in rejected:
                continue
            inspection = _strict_contained_path(canonical_root, root_fd, Path(path))
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
    finally:
        _close_fd(root_fd)


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
