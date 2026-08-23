# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioral tests for the blueprint gate's defect paths and fail-closed wiring."""

from __future__ import annotations

import hashlib
import importlib.util
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


checker = load("check_blueprint_scaffold", ROOT / "tools/analysis/check_blueprint_scaffold.py")
suite = load("run_architecture_checks", ROOT / "tools/analysis/run_architecture_checks.py")


def scaffold(root: Path, manifest_lines: list[str], files: dict[str, str]) -> Path:
    """Create a fixture repository with a blueprint manifest and selected files."""
    for relpath, content in files.items():
        path = root / relpath
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
    manifest = root / checker.MANIFEST_RELPATH
    manifest.parent.mkdir(parents=True, exist_ok=True)
    manifest.write_text("".join(f"{line}\n" for line in manifest_lines), encoding="utf-8")
    return root


def test_gate_reports_every_invariant_it_owns(tmp_path: Path) -> None:
    root = scaffold(
        tmp_path / "repo",
        ["real.txt", "real.txt", "/outside.txt", "../outside.txt", "hollow.txt"],
        {"real.txt": "content\n", "hollow.txt": ""},
    )

    errors = suite._blueprint_scaffold(root)

    assert any("real.txt is listed more than once" in error for error in errors)
    assert any("/outside.txt is absolute or escapes" in error for error in errors)
    assert any("../outside.txt is absolute or escapes" in error for error in errors)
    assert any("hollow.txt is materialized but unexpectedly empty" in error for error in errors)
    assert len(errors) == 4


def test_gate_ignores_unmaterialized_paths_owned_by_the_ratchet(tmp_path: Path) -> None:
    root = scaffold(
        tmp_path / "repo",
        ["real.txt", "not-written-yet.txt"],
        {"real.txt": "content\n"},
    )

    assert suite._blueprint_scaffold(root) == []


def test_absent_and_empty_manifests_fail_closed(tmp_path: Path) -> None:
    root = scaffold(tmp_path / "repo", [], {})

    assert suite._blueprint_scaffold(root) == [
        f"blueprint manifest lists no paths: {checker.MANIFEST_RELPATH}"
    ]

    (root / checker.MANIFEST_RELPATH).unlink()
    assert suite._blueprint_scaffold(root) == [
        f"blueprint manifest is missing: {checker.MANIFEST_RELPATH}"
    ]


def test_every_gated_defect_key_has_a_message() -> None:
    gated = set(checker.DEFECT_KEYS) - set(suite._BLUEPRINT_UNGATED)

    assert gated == set(suite._BLUEPRINT_DEFECT_MESSAGES)
    assert set(checker.DEFECT_KEYS) >= suite._BLUEPRINT_UNGATED


def test_unsafe_paths_are_never_counted_as_materialized(tmp_path: Path) -> None:
    outside = tmp_path / "outside.txt"
    outside.write_text("outside\n", encoding="utf-8")
    root = scaffold(
        tmp_path / "repo", ["real.txt", str(outside), "../outside.txt"], {"real.txt": "content\n"}
    )

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["unsafe_paths"] == ["../outside.txt", str(outside)]
    assert result["missing_paths"] == []
    assert result["blueprint_path_count"] == 3
    assert result["materialized_path_count"] == 1
    assert result["coverage_percent"] == 33.3333


def test_noncanonical_aliases_fail_closed_on_canonical_identity(
    tmp_path: Path, monkeypatch
) -> None:
    root = scaffold(
        tmp_path / "repo",
        [
            "a.txt",
            "./a.txt",
            "a.txt/",
            "nested//value.txt",
            "nested/./value.txt",
            "a.txt",
        ],
        {"a.txt": "safe\n", "nested/value.txt": "safe\n"},
    )
    original_inspection = checker._strict_contained_path
    inspected: list[str] = []

    def tracking_inspection(canonical_root, root_fd, relative):
        inspected.append(relative.as_posix())
        return original_inspection(canonical_root, root_fd, relative)

    monkeypatch.setattr(checker, "_strict_contained_path", tracking_inspection)

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["duplicate_paths"] == ["a.txt", "nested/value.txt"]
    assert result["noncanonical_paths"] == [
        "./a.txt",
        "a.txt/",
        "nested/./value.txt",
        "nested//value.txt",
    ]
    assert result["unsafe_paths"] == []
    assert result["blueprint_path_count"] == 6
    assert result["materialized_path_count"] == 1
    assert result["coverage_percent"] == 16.6667
    assert inspected == ["a.txt"]
    assert checker.has_failures(result)
    assert suite._blueprint_scaffold(root) == [
        "a.txt is listed more than once in the blueprint manifest",
        "nested/value.txt is listed more than once in the blueprint manifest",
        "./a.txt does not use the canonical POSIX repository-relative spelling",
        "a.txt/ does not use the canonical POSIX repository-relative spelling",
        "nested/./value.txt does not use the canonical POSIX repository-relative spelling",
        "nested//value.txt does not use the canonical POSIX repository-relative spelling",
    ]


def test_regular_manifest_and_nested_files_remain_safe(tmp_path: Path) -> None:
    root = scaffold(
        tmp_path / "repo",
        ["top-level.txt", "nested/deeper/value.txt"],
        {"top-level.txt": "top\n", "nested/deeper/value.txt": "nested\n"},
    )

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["manifest_errors"] == []
    assert result["unsafe_paths"] == []
    assert result["missing_paths"] == []
    assert result["materialized_path_count"] == 2
    assert result["coverage_percent"] == 100.0


def test_listed_leaf_and_ancestor_symlinks_are_unsafe(tmp_path: Path) -> None:
    outside_leaf = tmp_path / "outside-leaf.txt"
    outside_leaf.write_text("outside\n", encoding="utf-8")
    outside_directory = tmp_path / "outside-directory"
    outside_directory.mkdir()
    (outside_directory / "value.txt").write_text("outside\n", encoding="utf-8")
    root = scaffold(
        tmp_path / "repo",
        [
            "leaf-link.txt",
            "directory-link/value.txt",
            "internal-link/value.txt",
            "nested/value.txt",
        ],
        {"real/value.txt": "target\n", "nested/value.txt": "safe\n"},
    )
    (root / "leaf-link.txt").symlink_to(outside_leaf)
    (root / "directory-link").symlink_to(outside_directory, target_is_directory=True)
    (root / "internal-link").symlink_to(root / "real", target_is_directory=True)

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["unsafe_paths"] == [
        "directory-link/value.txt",
        "internal-link/value.txt",
        "leaf-link.txt",
    ]
    assert result["missing_paths"] == []
    assert result["materialized_path_count"] == 1
    assert result["coverage_percent"] == 25.0


def test_manifest_leaf_symlink_is_rejected(tmp_path: Path) -> None:
    root = tmp_path / "repo"
    manifest = root / checker.MANIFEST_RELPATH
    manifest.parent.mkdir(parents=True)
    outside = tmp_path / "outside-manifest.txt"
    outside.write_text("real.txt\n", encoding="utf-8")
    manifest.symlink_to(outside)

    result = checker.check(root, manifest)

    assert result["blueprint_path_count"] == 0
    assert result["manifest_errors"] == [
        f"blueprint manifest is unsafe: {checker.MANIFEST_RELPATH} (path contains a symbolic link)"
    ]
    assert suite._blueprint_scaffold(root) == result["manifest_errors"]


def test_manifest_ancestor_symlink_is_rejected(tmp_path: Path) -> None:
    root = tmp_path / "repo"
    root.mkdir()
    outside_docs = tmp_path / "outside-docs"
    outside_docs.mkdir()
    (outside_docs / "production-monorepo-paths.txt").write_text("real.txt\n", encoding="utf-8")
    (root / "docs").symlink_to(outside_docs, target_is_directory=True)

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["blueprint_path_count"] == 0
    assert result["manifest_errors"] == [
        f"blueprint manifest is unsafe: {checker.MANIFEST_RELPATH} (path contains a symbolic link)"
    ]
    assert suite._blueprint_scaffold(root) == result["manifest_errors"]


def test_manifest_ancestor_swap_stays_on_opened_directory(tmp_path: Path, monkeypatch) -> None:
    manifest_contents = b"safe.txt\n"
    root = scaffold(tmp_path / "repo", ["safe.txt"], {"safe.txt": "safe\n"})
    manifest = root / checker.MANIFEST_RELPATH
    outside_docs = tmp_path / "outside-docs"
    outside_manifest = outside_docs / "blueprint/production-monorepo-paths.txt"
    outside_manifest.parent.mkdir(parents=True)
    outside_manifest.write_bytes(b"external.txt\n")
    original_open = checker.os.open
    swapped = False

    def racing_open(path, flags, mode=0o777, *, dir_fd=None):
        nonlocal swapped
        if path == manifest.name and dir_fd is not None and not swapped:
            (root / "docs").rename(root / "docs-original")
            (root / "docs").symlink_to(outside_docs, target_is_directory=True)
            swapped = True
        return original_open(path, flags, mode, dir_fd=dir_fd)

    monkeypatch.setattr(checker.os, "open", racing_open)

    result = checker.check(root, manifest)

    assert swapped
    assert result["manifest_errors"] == []
    assert result["manifest_sha256"] == hashlib.sha256(manifest_contents).hexdigest()
    assert result["materialized_path_count"] == 1


def test_manifest_fifo_race_is_rejected_before_any_read(tmp_path: Path, monkeypatch) -> None:
    root = scaffold(tmp_path / "repo", ["safe.txt"], {"safe.txt": "safe\n"})
    manifest = root / checker.MANIFEST_RELPATH
    original_open = checker.os.open
    original_fdopen = checker.os.fdopen
    raced = False
    fdopen_calls = 0

    def racing_open(path, flags, mode=0o777, *, dir_fd=None):
        nonlocal raced
        if path == manifest.name and dir_fd is not None and not raced:
            manifest.rename(manifest.with_suffix(".original"))
            os.mkfifo(manifest)
            raced = True
        return original_open(path, flags, mode, dir_fd=dir_fd)

    def tracking_fdopen(*args, **kwargs):
        nonlocal fdopen_calls
        fdopen_calls += 1
        return original_fdopen(*args, **kwargs)

    monkeypatch.setattr(checker.os, "open", racing_open)
    monkeypatch.setattr(checker.os, "fdopen", tracking_fdopen)

    result = checker.check(root, manifest)

    assert raced
    assert fdopen_calls == 0
    assert result["manifest_errors"] == [
        f"blueprint manifest is unsafe: {checker.MANIFEST_RELPATH} "
        "(path changed before it was read)"
    ]


def test_manifest_inspection_oserror_fails_closed_before_any_read(
    tmp_path: Path, monkeypatch
) -> None:
    root = scaffold(tmp_path / "repo", ["safe.txt"], {"safe.txt": "safe\n"})
    manifest = root / checker.MANIFEST_RELPATH
    original_stat = checker.os.stat
    original_fdopen = checker.os.fdopen
    fdopen_calls = 0

    def refusing_stat(path, *args, **kwargs):
        if path == manifest.name and kwargs.get("dir_fd") is not None:
            raise PermissionError("inspection denied")
        return original_stat(path, *args, **kwargs)

    def tracking_fdopen(*args, **kwargs):
        nonlocal fdopen_calls
        fdopen_calls += 1
        return original_fdopen(*args, **kwargs)

    monkeypatch.setattr(checker.os, "stat", refusing_stat)
    monkeypatch.setattr(checker.os, "fdopen", tracking_fdopen)

    result = checker.check(root, manifest)

    assert fdopen_calls == 0
    assert result["manifest_errors"] == [
        f"blueprint manifest is unsafe: {checker.MANIFEST_RELPATH} "
        "(path could not be inspected safely)"
    ]


def test_embedded_nul_path_is_stably_unsafe(tmp_path: Path) -> None:
    root = scaffold(
        tmp_path / "repo",
        ["safe.txt", "bad\x00name.txt"],
        {"safe.txt": "safe\n"},
    )

    result = checker.check(root, root / checker.MANIFEST_RELPATH)

    assert result["manifest_errors"] == []
    assert result["unsafe_paths"] == ["bad\x00name.txt"]
    assert result["missing_paths"] == []
    assert result["materialized_path_count"] == 1
    assert result["coverage_percent"] == 50.0


def test_manifest_outside_repository_root_is_rejected(tmp_path: Path) -> None:
    root = tmp_path / "repo"
    root.mkdir()
    outside = tmp_path / "outside-manifest.txt"
    outside.write_text("real.txt\n", encoding="utf-8")

    result = checker.check(root, outside)

    assert result["blueprint_path_count"] == 0
    assert result["manifest_errors"] == [
        f"blueprint manifest is outside the repository root: {outside}"
    ]


def test_cli_exit_codes_share_empty_manifest_failure(tmp_path: Path, monkeypatch, capsys) -> None:
    empty_root = scaffold(tmp_path / "empty", [], {})
    monkeypatch.setattr(sys, "argv", ["check_blueprint_scaffold", "--root", str(empty_root)])

    assert checker.main() == 1
    empty_output = capsys.readouterr()
    assert f"blueprint manifest lists no paths: {checker.MANIFEST_RELPATH}" in empty_output.err

    valid_root = scaffold(tmp_path / "valid", ["real.txt"], {"real.txt": "content\n"})
    monkeypatch.setattr(sys, "argv", ["check_blueprint_scaffold", "--root", str(valid_root)])

    assert checker.main() == 0
    valid_output = capsys.readouterr()
    assert valid_output.err == ""
