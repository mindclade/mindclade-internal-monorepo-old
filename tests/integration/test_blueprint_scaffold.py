"""Repository-level target-state scaffold coverage."""

from __future__ import annotations

import importlib.util
from pathlib import Path


def _load_checker(root: Path):
    path = root / "tools/analysis/check_blueprint_scaffold.py"
    spec = importlib.util.spec_from_file_location("check_blueprint_scaffold", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_every_blueprint_path_is_materialized() -> None:
    root = Path(__file__).resolve().parents[2]
    module = _load_checker(root)
    manifest = root / "docs/blueprint/production-monorepo-paths.txt"
    result = module.check(root, manifest)
    assert result["coverage_percent"] == 100.0
    assert result["duplicate_paths"] == []
    assert result["missing_paths"] == []
    assert result["unexpected_empty_paths"] == []
    assert result["unsafe_paths"] == []
