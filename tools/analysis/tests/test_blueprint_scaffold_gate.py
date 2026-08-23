# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioral tests for the blueprint gate's defect paths and fail-closed wiring."""

from __future__ import annotations

import importlib.util
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
