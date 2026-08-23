# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import subprocess
from pathlib import Path

import pytest

from tools.dev import git_hygiene_report

GIT = git_hygiene_report._git_binary()


def _git(root: Path, *arguments: str) -> str:
    return subprocess.run(
        [GIT, "-C", str(root), *arguments],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def _repository(tmp_path: Path) -> Path:
    remote = tmp_path / "remote.git"
    subprocess.run([GIT, "init", "--bare", str(remote)], check=True, capture_output=True)
    root = tmp_path / "repo"
    subprocess.run([GIT, "init", "-b", "main", str(root)], check=True, capture_output=True)
    _git(root, "config", "user.name", "Test User")
    _git(root, "config", "user.email", "test@example.com")
    (root / "tracked.txt").write_text("base\n", encoding="utf-8")
    _git(root, "add", "tracked.txt")
    _git(root, "commit", "-m", "base")
    _git(root, "remote", "add", "origin", str(remote))
    _git(root, "push", "-u", "origin", "main")
    _git(root, "fetch", "origin", "main")
    return root


def test_reports_unique_commits_and_dirty_files_without_mutation(tmp_path: Path) -> None:
    root = _repository(tmp_path)
    worktree = tmp_path / "feature"
    _git(root, "worktree", "add", "-b", "feature", str(worktree), "main")
    (worktree / "feature.txt").write_text("unique\n", encoding="utf-8")
    _git(worktree, "add", "feature.txt")
    _git(worktree, "commit", "-m", "unique")
    (worktree / "dirty.txt").write_text("preserve me\n", encoding="utf-8")
    before_report_refs = _git(root, "show-ref")

    payload = git_hygiene_report.build_report(root)

    feature = next(item for item in payload["branches"] if item["ref"] == "refs/heads/feature")
    reported_worktree = next(item for item in payload["worktrees"] if item["path"] == str(worktree))
    assert feature["unique_commit_count"] == 1
    assert len(feature["unique_commits"]) == 1
    assert reported_worktree["dirty_files"] == [{"path": "dirty.txt", "status": "??"}]
    assert payload["mode"] == "report-only"
    assert payload["mutation_performed"] is False
    assert _git(root, "show-ref") == before_report_refs
    assert (worktree / "dirty.txt").read_text(encoding="utf-8") == "preserve me\n"


def test_html_escapes_paths(tmp_path: Path) -> None:
    payload = {
        "branches": [
            {"ref": "refs/heads/<unsafe>", "head_sha": "a" * 40, "unique_commit_count": 1}
        ],
        "worktrees": [
            {
                "path": "/tmp/<unsafe>",
                "dirty_files": [],
                "unique_commit_count": 0,
                "locked": False,
            }
        ],
    }
    rendered = git_hygiene_report.render_html(payload)
    assert "<unsafe>" not in rendered
    assert "&lt;unsafe&gt;" in rendered


def test_rejects_option_like_baseline_without_mutation(tmp_path: Path) -> None:
    root = _repository(tmp_path)
    before = _git(root, "show-ref")
    with pytest.raises(git_hygiene_report.HygieneReportError, match="baseline reference"):
        git_hygiene_report.build_report(root, baseline="--all")
    assert _git(root, "show-ref") == before
