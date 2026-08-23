# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import shutil
import time
from pathlib import Path

import pytest

from ci.presubmit import disk_preflight


def _usage(free_gib: float) -> shutil._ntuple_diskusage:
    total = 512 * disk_preflight.GIB
    free = int(free_gib * disk_preflight.GIB)
    return shutil._ntuple_diskusage(total=total, used=total - free, free=free)


def _output_base(root: Path, name: str, workspace: Path) -> Path:
    base = root / name
    base.mkdir(parents=True)
    # Bazel writes the workspace path with no trailing newline.
    (base / disk_preflight.WORKSPACE_MARKER).write_text(str(workspace), encoding="utf-8")
    return base


@pytest.fixture
def isolated_roots(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> Path:
    """Point root discovery at a scratch directory instead of the developer's real caches."""
    root = tmp_path / "_bazel_scratch"
    root.mkdir()
    monkeypatch.setenv("TEST_TMPDIR", str(root))
    monkeypatch.delenv("USER", raising=False)
    monkeypatch.delenv("LOGNAME", raising=False)
    return root


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
    # A sibling worktree's target/ is the fastest thing to reclaim, and the operator staring at
    # the failure usually does not know how many of them exist.
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


# ---------------------------------------------------------------------------------------
# Output-root discovery
#
# The original implementation resolved exactly one root, and on macOS it resolved the one Bazel
# had stopped using. It therefore reported a stale root's contents as the entire Bazel
# footprint. These pin both platforms' full probe list.
# ---------------------------------------------------------------------------------------


def test_darwin_probes_both_historical_output_user_roots(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(disk_preflight.sys, "platform", "darwin")
    monkeypatch.setenv("USER", "someone")
    monkeypatch.setenv("HOME", "/Users/someone")
    monkeypatch.delenv("TEST_TMPDIR", raising=False)
    monkeypatch.delenv("XDG_CACHE_HOME", raising=False)
    roots = {str(root) for root in disk_preflight.bazel_output_user_roots()}
    # Bazel's macOS default moved between these two. A host that has run both releases has both.
    assert "/Users/someone/Library/Caches/bazel/_bazel_someone" in roots
    assert "/private/var/tmp/_bazel_someone" in roots


def test_linux_probes_xdg_cache_and_var_tmp(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(disk_preflight.sys, "platform", "linux")
    monkeypatch.setenv("USER", "someone")
    monkeypatch.setenv("HOME", "/home/someone")
    monkeypatch.setenv("XDG_CACHE_HOME", "/home/someone/.cache-alt")
    monkeypatch.delenv("TEST_TMPDIR", raising=False)
    roots = {str(root) for root in disk_preflight.bazel_output_user_roots()}
    # The old code appended "bazel" and stopped, missing the per-user component entirely.
    assert "/home/someone/.cache-alt/bazel/_bazel_someone" in roots
    assert "/var/tmp/_bazel_someone" in roots


def test_test_tmpdir_is_probed_first(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    monkeypatch.setenv("TEST_TMPDIR", str(tmp_path))
    monkeypatch.setenv("USER", "someone")
    assert disk_preflight.bazel_output_user_roots()[0] == tmp_path


# ---------------------------------------------------------------------------------------
# Output-base enumeration and orphan classification
# ---------------------------------------------------------------------------------------


def test_orphaned_base_is_the_one_whose_workspace_is_gone(
    isolated_roots: Path, tmp_path: Path
) -> None:
    live_workspace = tmp_path / "live-checkout"
    live_workspace.mkdir()
    _output_base(isolated_roots, "aaaa", live_workspace)
    _output_base(isolated_roots, "bbbb", tmp_path / "deleted-checkout")

    bases = disk_preflight.bazel_output_bases(disk_preflight.bazel_output_user_roots())
    by_name = {base.path.name: base for base in bases}
    assert by_name["aaaa"].state == "live"
    assert not by_name["aaaa"].orphaned
    assert by_name["bbbb"].state == "ORPHAN"
    assert by_name["bbbb"].orphaned


def test_unreadable_marker_is_unknown_rather_than_orphaned(isolated_roots: Path) -> None:
    # Calling a base orphaned on absent evidence would invite an `rm -rf` of a live tree.
    base = isolated_roots / "cccc"
    base.mkdir()
    (base / disk_preflight.WORKSPACE_MARKER).write_text("", encoding="utf-8")
    (bases,) = disk_preflight.bazel_output_bases(disk_preflight.bazel_output_user_roots())
    assert bases.state == "unknown"
    assert not bases.orphaned


def test_shared_root_directories_are_not_counted_as_bases(isolated_roots: Path) -> None:
    for name in disk_preflight.NON_BASE_ENTRIES:
        (isolated_roots / name).mkdir()
    assert disk_preflight.bazel_output_bases(disk_preflight.bazel_output_user_roots()) == []


def test_directory_without_a_marker_is_not_a_base(isolated_roots: Path) -> None:
    (isolated_roots / "not-a-base").mkdir()
    assert disk_preflight.bazel_output_bases(disk_preflight.bazel_output_user_roots()) == []


def test_repository_download_cache_is_a_sized_candidate(
    isolated_roots: Path, tmp_path: Path
) -> None:
    # It was 3.9 GiB on the reference host -- larger than any single live output base there --
    # and unlike a base it costs refetches rather than rebuilds to reclaim.
    (isolated_roots / "cache").mkdir()
    paths = {candidate.path for candidate in disk_preflight.reclaim_candidates(tmp_path)}
    assert (isolated_roots / "cache").resolve() in paths


# ---------------------------------------------------------------------------------------
# What the failure message says about Bazel
# ---------------------------------------------------------------------------------------


def test_shortfall_names_orphaned_output_bases_and_their_dead_workspaces(
    monkeypatch: pytest.MonkeyPatch, isolated_roots: Path, tmp_path: Path
) -> None:
    dead = tmp_path / "deleted-worktree"
    base = _output_base(isolated_roots, "dddd", dead)
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(1))
    (failure,) = disk_preflight.check(tmp_path, 16 * disk_preflight.GIB)
    assert "1 of them belong to workspaces that no longer exist" in failure
    assert str(base) in failure
    assert str(dead) in failure
    assert "python3 ci/presubmit/disk_preflight.py --report" in failure


def test_bazel_section_precedes_the_cargo_table(
    monkeypatch: pytest.MonkeyPatch, isolated_roots: Path, tmp_path: Path
) -> None:
    # The first version led with Cargo because Cargo was all it could size inside its budget. A
    # reader who acted on that order reclaimed 28 GiB and left 98 GiB of unreachable output
    # bases untouched.
    _output_base(isolated_roots, "eeee", tmp_path / "deleted-worktree")
    sibling = tmp_path / ".claude" / "worktrees" / "agent-deadbeef" / "target"
    sibling.mkdir(parents=True)
    monkeypatch.setattr(disk_preflight.shutil, "disk_usage", lambda _path: _usage(1))
    (failure,) = disk_preflight.check(tmp_path, 16 * disk_preflight.GIB)
    assert failure.index("Bazel output bases on this filesystem") < failure.index(
        "Other reclaim candidates on this filesystem"
    )


def test_report_is_silent_about_bases_when_there_are_none(tmp_path: Path) -> None:
    assert disk_preflight.output_base_report([]) == []


# ---------------------------------------------------------------------------------------
# The sizing budget
# ---------------------------------------------------------------------------------------


def test_sizing_budget_is_shared_rather_than_per_candidate(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    # The reference host has thirty-five sibling worktrees. A 2s-per-candidate walk is over a
    # minute of diagnostics bolted onto an abort, on a filesystem that is already failing.
    candidate_count = 20
    budget = 0.5
    shares: list[float] = []

    def spend_the_whole_share(path: Path, deadline: float) -> tuple[int, bool]:
        share = deadline - time.monotonic()
        shares.append(share)
        time.sleep(max(0.0, share))
        return 1, True

    monkeypatch.setattr(disk_preflight, "directory_size", spend_the_whole_share)
    candidates = [
        disk_preflight.Candidate(tmp_path, f"candidate {index}")
        for index in range(candidate_count)
    ]
    started = time.monotonic()
    sized = disk_preflight.size_candidates(candidates, total_budget=budget)
    elapsed = time.monotonic() - started

    assert len(sized) == candidate_count
    # The whole point: bounded by the shared budget, not by count x per-candidate ceiling.
    assert elapsed < budget * 3
    assert elapsed < candidate_count * disk_preflight.SIZE_SCAN_SECONDS
    # No candidate is starved to nothing, and none exceeds the per-directory ceiling. The 0.99
    # slack absorbs the microseconds that pass between computing the deadline and reading the
    # clock inside the callee; without it this asserts on scheduler noise, not on the policy.
    assert all(share >= disk_preflight.SIZE_SCAN_MIN_SECONDS * 0.99 for share in shares)
    assert all(share <= disk_preflight.SIZE_SCAN_SECONDS for share in shares)


def test_exhausted_budget_still_gives_every_candidate_its_floor(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    # A global-only budget is spent by whichever tree is walked first, and every candidate after
    # it reports "0.0 GiB" -- which reads as "these are empty", not "these were not measured".
    shares: list[float] = []

    def record(path: Path, deadline: float) -> tuple[int, bool]:
        shares.append(deadline - time.monotonic())
        return 0, True

    monkeypatch.setattr(disk_preflight, "directory_size", record)
    candidates = [disk_preflight.Candidate(tmp_path, "x") for _ in range(5)]
    disk_preflight.size_candidates(candidates, total_budget=-1.0)
    assert shares
    assert all(share >= disk_preflight.SIZE_SCAN_MIN_SECONDS * 0.99 for share in shares)


def test_report_mode_prints_totals_and_the_reclamation_pointer(
    isolated_roots: Path, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    live_workspace = tmp_path / "live-checkout"
    live_workspace.mkdir()
    _output_base(isolated_roots, "ffff", live_workspace)
    orphan = _output_base(isolated_roots, "gggg", tmp_path / "deleted-checkout")
    (orphan / "payload").write_bytes(b"\0" * 8192)

    assert disk_preflight.report(tmp_path) == 0
    printed = capsys.readouterr().out
    assert str(isolated_roots) in printed
    assert "ORPHAN" in printed
    assert "reclaimable now: 1 base(s) whose workspace no longer exists" in printed
    assert "ci/bazel/README.md" in printed
