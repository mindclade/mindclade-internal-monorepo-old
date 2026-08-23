#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Assert free-space headroom before the expensive presubmit lanes run.

WHY THIS EXISTS
===============
A host ran out of disk in the middle of a review and took down thirteen concurrent agents at
once. It did not surface as a build error. It surfaced as watchdog timeouts and an ENOSPC on
the harness's own task-output file, so for a long time it read as thirteen unrelated
regressions rather than one exhausted filesystem. The root cause was roughly 28 GiB of Cargo
`target/` output spread over eight worktrees.

The defect is not that the disk filled. The defect is that nothing *asserted* headroom, so the
failure arrived disguised as something else. A cheap `statvfs` before the lanes that consume
tens of gigabytes converts an hour of misattributed debugging into one sentence naming the
directories to delete.

WHY IT IS A PREFLIGHT AND NOT A CLEANUP
=======================================
This module never deletes anything. Reclaim candidates on a shared host belong to other
workers -- a sibling worktree's `target/` is another agent's warm build cache, and a Bazel
output base is expensive to rebuild. Naming them and refusing to start is correct; removing
them behind the operator's back is not.

THRESHOLD DERIVATION
====================
`DEFAULT_MIN_FREE_BYTES` is the measured cost of one full run on one workspace, rounded up:

  Bazel output base for one workspace  4.6 GiB   measured: the output user root held 139 GiB
                                                 across ~30 live worktrees. Bazel keys the
                                                 output base on the workspace path, so every
                                                 worktree pays this again and nothing bounds
                                                 the total.
  Go build cache + module cache        3.9 GiB   measured: `go env GOCACHE` = 2.9 GiB,
                                                 `go env GOMODCACHE` = 978 MiB
  Cargo target/ for one worktree       3.5 GiB   measured: 28 GiB across 8 worktrees running
                                                 `--workspace --all-targets --all-features`
  Bazel disk cache                     1.0 GiB   bounded by `ci/common/bazel_disk_cache.py`
                                                 (`--experimental_disk_cache_gc_max_size=1G`)
  uv `.venv`, `node_modules`, pnpm     ~1.0 GiB

  subtotal                            ~14 GiB

The subtotal is what a run *consumes*; a floor set exactly there still lets a run finish with
zero bytes left, which is the state that corrupted the task-output file. The default adds
roughly two gigabytes of slack so that exhaustion is reported here, by name, rather than by a
truncated write somewhere downstream.

RELATIONSHIP TO `ci/common/prepare_nix_runner.sh`
=================================================
That script already asserts 50 GiB free on `/` -- but it refuses to run anywhere except an
ephemeral GitHub-hosted Linux runner, and it looks only at `/`. The outage this module was
written for happened on a developer host, on the data volume, with the Bazel output user root
and thirty worktrees' `target/` directories on it. This check runs wherever the pipeline runs
and probes every filesystem the lanes actually write to. The 50 GiB hosted floor comfortably
satisfies the floor here, so the two do not contradict each other.

The number is deliberately a single reviewed constant rather than a percentage. A percentage of
a 460 GiB volume and a percentage of a 40 GiB runner disk are not the same policy, and the
quantity that matters is absolute: how many bytes the next hour of work is going to write.
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
import time
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]

GIB = 1024**3

# See THRESHOLD DERIVATION above. Raising this is a policy change; lowering it to make a
# constrained host pass is the class of edit this file exists to prevent.
DEFAULT_MIN_FREE_BYTES = 16 * GIB

# Wall-clock ceiling for the diagnostic sizing walk. The check itself is a `statvfs` and costs
# microseconds; only the failure path measures reclaim candidates, and it must not turn an
# out-of-disk abort into a hang on a filesystem that is already struggling.
#
# Budgeted PER CANDIDATE rather than for the walk as a whole. A single shared deadline is spent
# entirely by whichever directory happens to be scanned first -- the Bazel output base, which
# held 139 GiB in hundreds of thousands of files -- and every candidate after it then reports
# "0.0 GiB", which is worse than reporting nothing.
SIZE_SCAN_SECONDS = 2.0


@dataclass(frozen=True)
class Candidate:
    """A directory an operator can delete to reclaim space, with why it is safe to consider."""

    path: Path
    what: str


@dataclass(frozen=True)
class Mount:
    """One distinct filesystem that the expensive lanes write to."""

    device: int
    probe: Path
    free_bytes: int
    total_bytes: int


def _bazel_output_user_root() -> Path | None:
    """Resolve Bazel's output user root without shelling out to Bazel.

    Bazel picks `$TEST_TMPDIR`, then `$XDG_CACHE_HOME/bazel`, then a platform default. Invoking
    `bazel info output_base` here would be self-defeating: starting a server is exactly the
    expensive work this preflight is supposed to gate.
    """
    override = os.environ.get("TEST_TMPDIR")
    if override:
        return Path(override)
    user = os.environ.get("USER") or os.environ.get("LOGNAME")
    if sys.platform == "darwin":
        return Path("/private/var/tmp") / f"_bazel_{user}" if user else None
    cache_home = os.environ.get("XDG_CACHE_HOME")
    if cache_home:
        return Path(cache_home) / "bazel"
    home = os.environ.get("HOME")
    return Path(home) / ".cache" / "bazel" if home else None


def _go_cache_dirs() -> Iterator[Candidate]:
    """Yield the Go caches without invoking `go env`, which needs the toolchain on PATH."""
    home = os.environ.get("HOME")
    build = os.environ.get("GOCACHE")
    if not build and home:
        if sys.platform == "darwin":
            build = str(Path(home) / "Library" / "Caches" / "go-build")
        else:
            cache_home = os.environ.get("XDG_CACHE_HOME") or str(Path(home) / ".cache")
            build = str(Path(cache_home) / "go-build")
    if build:
        yield Candidate(Path(build), "Go build cache (`go clean -cache`)")
    modules = os.environ.get("GOMODCACHE")
    if not modules:
        gopath = os.environ.get("GOPATH") or (str(Path(home) / "go") if home else None)
        modules = str(Path(gopath) / "pkg" / "mod") if gopath else None
    if modules:
        yield Candidate(Path(modules), "Go module cache (`go clean -modcache`)")


def _cargo_target_dirs(repo: Path) -> Iterator[Candidate]:
    """Yield this worktree's `target/` plus every sibling worktree's.

    Sibling worktrees are the interesting case and the one that caused the outage: each one
    carries an independent multi-gigabyte `target/`, and the operator staring at a full disk
    usually has no idea how many of them exist.
    """
    override = os.environ.get("CARGO_TARGET_DIR")
    if override:
        yield Candidate(Path(override), "Cargo target directory (CARGO_TARGET_DIR)")
    yield Candidate(repo / "target", "Cargo target/ for this worktree")
    # `.claude/worktrees/*/target` is where the agent harness places isolated checkouts. Listed
    # explicitly rather than discovered by a filesystem-wide search, which would itself be slow
    # on the loaded host this runs on.
    worktrees = repo / ".claude" / "worktrees"
    if worktrees.is_dir():
        try:
            entries = sorted(worktrees.iterdir())
        except OSError:
            entries = []
        for entry in entries:
            target = entry / "target"
            if target.is_dir():
                yield Candidate(target, f"Cargo target/ for worktree {entry.name}")


def reclaim_candidates(repo: Path) -> list[Candidate]:
    """Every directory worth naming in a failure message, deduplicated, existing only."""
    candidates: list[Candidate] = []
    seen: set[Path] = set()
    output_root = _bazel_output_user_root()
    ordered: list[Candidate] = []
    if output_root is not None:
        ordered.append(Candidate(output_root, "Bazel output base (`bazel clean --expunge`)"))
    ordered.extend(_go_cache_dirs())
    ordered.extend(_cargo_target_dirs(repo))
    ordered.append(Candidate(repo / ".venv", "uv virtual environment (`rm -rf .venv`)"))
    ordered.append(Candidate(repo / "node_modules", "pnpm install tree"))
    for candidate in ordered:
        try:
            resolved = candidate.path.resolve()
        except OSError:
            continue
        if resolved in seen or not candidate.path.is_dir():
            continue
        seen.add(resolved)
        candidates.append(Candidate(resolved, candidate.what))
    return candidates


def directory_size(path: Path, deadline: float) -> tuple[int, bool]:
    """Sum a tree's apparent size, stopping at `deadline`.

    Returns the bytes counted and whether the walk was truncated, so the caller can render
    ">= 4.1 GiB" instead of a confidently wrong total. Truncation is normal here: these trees
    hold hundreds of thousands of small files and the host is by definition under pressure.
    """
    total = 0
    truncated = False
    stack = [path]
    while stack:
        if time.monotonic() > deadline:
            return total, True
        current = stack.pop()
        try:
            with os.scandir(current) as entries:
                for entry in entries:
                    try:
                        if entry.is_symlink():
                            continue
                        if entry.is_dir():
                            stack.append(Path(entry.path))
                        else:
                            total += entry.stat(follow_symlinks=False).st_size
                    except OSError:
                        truncated = True
        except OSError:
            truncated = True
    return total, truncated


def _format_bytes(value: int) -> str:
    return f"{value / GIB:.1f} GiB"


def probe_mounts(repo: Path, candidates: list[Candidate]) -> list[Mount]:
    """One `Mount` per distinct filesystem the lanes write to.

    Grouping by `st_dev` matters: on a laptop the repository, the Go caches, and the Bazel
    output base are usually one volume, and reporting the same free-space number three times
    would read as three independent problems.
    """
    mounts: dict[int, Mount] = {}
    probes = [repo, *(candidate.path for candidate in candidates)]
    for probe in probes:
        try:
            device = probe.stat().st_dev
        except OSError:
            continue
        if device in mounts:
            continue
        try:
            usage = shutil.disk_usage(probe)
        except OSError:
            continue
        mounts[device] = Mount(
            device=device, probe=probe, free_bytes=usage.free, total_bytes=usage.total
        )
    return list(mounts.values())


def check(repo: Path, minimum_free_bytes: int) -> list[str]:
    """Return one message per filesystem below the floor; empty means the run may proceed."""
    candidates = reclaim_candidates(repo)
    failures = []
    for mount in probe_mounts(repo, candidates):
        if mount.free_bytes >= minimum_free_bytes:
            print(
                f"disk preflight: {mount.probe} has {_format_bytes(mount.free_bytes)} free "
                f"(floor {_format_bytes(minimum_free_bytes)})"
            )
            continue
        lines = [
            f"DISK-PREFLIGHT-001: {mount.probe} has {_format_bytes(mount.free_bytes)} free, "
            f"below the {_format_bytes(minimum_free_bytes)} floor one full run needs.",
            "",
            "This aborts before the Bazel, Cargo, and Go lanes rather than after, because "
            "exhausting the disk mid-run surfaces as watchdog timeouts and ENOSPC on unrelated "
            "files, not as a build error.",
            "",
            "Reclaim candidates on this filesystem, largest first:",
        ]
        sized = []
        for candidate in candidates:
            try:
                if candidate.path.stat().st_dev != mount.device:
                    continue
            except OSError:
                continue
            size, truncated = directory_size(candidate.path, time.monotonic() + SIZE_SCAN_SECONDS)
            sized.append((size, truncated, candidate))
        sized.sort(key=lambda item: item[0], reverse=True)
        if not sized:
            lines.append("  (none found; the space is held outside this repository's caches)")
        for size, truncated, candidate in sized:
            measurement = f"at least {_format_bytes(size)}" if truncated else _format_bytes(size)
            lines.append(f"  {measurement:>18}  {candidate.path}")
            lines.append(f"  {'':>18}  {candidate.what}")
        if any(truncated for _, truncated, _ in sized):
            lines.append("")
            lines.append(
                '  "at least" means the sizing walk hit its '
                f"{SIZE_SCAN_SECONDS:g}s per-directory budget; the real total is larger."
            )
        failures.append("\n".join(lines))
    return failures


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--min-free-gib",
        type=float,
        default=DEFAULT_MIN_FREE_BYTES / GIB,
        help="Free-space floor per filesystem, in GiB. See THRESHOLD DERIVATION in this module.",
    )
    parser.add_argument("--repo", type=Path, default=REPO)
    arguments = parser.parse_args(argv)
    if arguments.min_free_gib <= 0:
        parser.error("--min-free-gib must be positive; a zero floor is not a preflight")
    failures = check(arguments.repo, int(arguments.min_free_gib * GIB))
    for failure in failures:
        print(failure, file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
