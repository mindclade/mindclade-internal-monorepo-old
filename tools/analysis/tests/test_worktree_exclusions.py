# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

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


go_modules = load("check_go_modules", ROOT / "tools/analysis/check_go_modules.py")
license_headers = load("check_license_headers", ROOT / "tools/analysis/check_license_headers.py")
repository = load("validate_repository", ROOT / "tools/dev/validate_repository.py")


def test_go_module_check_ignores_nested_codex_checkout(tmp_path: Path) -> None:
    (tmp_path / "go.mod").write_text("module example.invalid/root\n", encoding="utf-8")
    nested = tmp_path / ".codex-worktrees/agent/go.mod"
    nested.parent.mkdir(parents=True)
    nested.write_text("module example.invalid/nested\n", encoding="utf-8")

    assert go_modules.check(tmp_path) == []


def test_license_scan_ignores_nested_codex_checkout(tmp_path: Path) -> None:
    source = tmp_path / "source.py"
    source.write_text("print('source')\n", encoding="utf-8")
    nested = tmp_path / ".codex-worktrees/agent/nested.py"
    nested.parent.mkdir(parents=True)
    nested.write_text("print('nested')\n", encoding="utf-8")

    assert license_headers.iter_sources(tmp_path, []) == [source]


def test_repository_walk_ignores_nested_codex_checkout(tmp_path: Path) -> None:
    source = tmp_path / "source.json"
    source.write_text("{}\n", encoding="utf-8")
    nested = tmp_path / ".codex-worktrees/agent/invalid.json"
    nested.parent.mkdir(parents=True)
    nested.write_text("not json\n", encoding="utf-8")

    assert list(repository._walk(tmp_path)) == [source]
