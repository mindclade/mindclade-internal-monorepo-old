# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import shutil
from pathlib import Path

import pytest

from ci.presubmit import disk_preflight


def _usage(free_gib: float) -> shutil._ntuple_diskusage:
    total = 512 * disk_preflight.GIB
    free = int(free_gib * disk_preflight.GIB)
    return shutil._ntuple_diskusage(total=total, used=total - free, free=free)


def test_ample_free_space_reports_no_failures(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(64))
    assert disk_preflight.check(tmp_path, 16 * disk_preflight.GIB) == []


def test_shortfall_names_the_filesystem_and_the_floor(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(0.75))
    failures = disk_preflight.check(tmp_path, 16 * disk_preflight.GIB)
    assert len(failures) == 1
    assert "DISK-PREFLIGHT-001" in failures[0]
    assert "0.8 GiB free" in failures[0]
    assert "16.0 GiB floor" in failures[0]


def test_shortfall_names_reclaim_candidates_that_exist(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    # A sibling worktree's target/ is the directory that actually filled the disk, and the
    # operator staring at the failure usually does not know how many of them exist.
    sibling = tmp_path / ".claude" / "worktrees" / "agent-deadbeef" / "target"
    sibling.mkdir(parents=True)
    (sibling / "blob").write_bytes(b"\0" * 4096)
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(1))
    failures = disk_preflight.check(tmp_path, 16 * disk_preflight.GIB)
    assert "agent-deadbeef" in failures[0]
    assert "Cargo target/ for worktree agent-deadbeef" in failures[0]


def test_candidates_skip_paths_that_do_not_exist(tmp_path: Path) -> None:
    # Naming a directory that is not there would send an operator chasing a phantom.
    for candidate in disk_preflight.reclaim_candidates(tmp_path):
        assert candidate.path.is_dir()


def test_one_message_per_filesystem_not_per_candidate(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    # The repository, the Go caches, and the Bazel output base are normally one volume.
    # Reporting the same shortfall three times reads as three independent problems.
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(1))
    monkeypatch.setattr(disk_preflight.Path, "stat", lambda self: _FixedStat())
    assert len(disk_preflight.check(tmp_path, 16 * disk_preflight.GIB)) == 1


class _FixedStat:
    st_dev = 1234
    st_size = 0


def test_directory_size_stops_at_its_deadline(tmp_path: Path) -> None:
    nested = tmp_path
    for level in range(6):
        nested = nested / f"level{level}"
        nested.mkdir()
        (nested / "blob").write_bytes(b"\0" * 1024)
    # A deadline already in the past must return immediately and say so, rather than walking a
    # tree on a filesystem that is by definition already under pressure.
    size, truncated = disk_preflight.directory_size(tmp_path, deadline=float("-inf"))
    assert truncated
    assert size == 0


def test_zero_floor_is_rejected() -> None:
    # "--min-free-gib 0" would turn the preflight into a no-op that still reports success.
    with pytest.raises(SystemExit):
        disk_preflight.main(["--min-free-gib", "0"])
