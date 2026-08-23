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


check_go_test_signal = load("check_go_test_signal", ROOT / "tools/analysis/check_go_test_signal.py")


def write(root: Path, relative: str, source: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(source, encoding="utf-8")


def test_accepts_direct_failure_signal(tmp_path: Path) -> None:
    write(
        tmp_path,
        "pkg/direct_test.go",
        'package pkg\nimport "testing"\nfunc TestDirect(t *testing.T) { t.Fatal("failed") }\n',
    )
    assert check_go_test_signal.check(tmp_path) == []


def test_accepts_subtest_signal(tmp_path: Path) -> None:
    write(
        tmp_path,
        "pkg/subtest_test.go",
        'package pkg\nimport "testing"\nfunc TestSubtest(t *testing.T) { t.Run("case", func(t *testing.T) { t.Error("failed") }) }\n',
    )
    assert check_go_test_signal.check(tmp_path) == []


def test_rejects_empty_subtest(tmp_path: Path) -> None:
    write(
        tmp_path,
        "pkg/subtest_test.go",
        'package pkg\nimport "testing"\nfunc TestSubtest(t *testing.T) { t.Run("case", func(t *testing.T) {}) }\n',
    )
    assert check_go_test_signal.check(tmp_path) == ["GO_TEST_SIGNAL_MISSING pkg/subtest_test.go"]


def test_rejects_helper_only_test(tmp_path: Path) -> None:
    write(
        tmp_path,
        "pkg/scaffold_test.go",
        'package pkg\nimport "testing"\nfunc TestScaffold(t *testing.T) { t.Helper() }\n',
    )
    assert check_go_test_signal.check(tmp_path) == ["GO_TEST_SIGNAL_MISSING pkg/scaffold_test.go"]


def test_ignores_signal_text_in_comments_and_literals(tmp_path: Path) -> None:
    write(
        tmp_path,
        "pkg/inert_test.go",
        """package pkg
import "testing"
// t.Fatal("comment")
func TestInert(t *testing.T) {
    t.Helper()
    _ = "t.Errorf(\"literal\")"
    _ = `t.Run("raw", nil)`
}
""",
    )
    assert check_go_test_signal.check(tmp_path) == ["GO_TEST_SIGNAL_MISSING pkg/inert_test.go"]


def test_ignores_agent_worktrees(tmp_path: Path) -> None:
    write(
        tmp_path,
        ".codex-worktrees/task/pkg/scaffold_test.go",
        'package pkg\nimport "testing"\nfunc TestScaffold(t *testing.T) { t.Helper() }\n',
    )
    assert check_go_test_signal.check(tmp_path) == []
