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
regressions rather than one exhausted filesystem.

The defect is not that the disk filled. The defect is that nothing *asserted* headroom, so the
failure arrived disguised as something else. A cheap `statvfs` before the lanes that consume
tens of gigabytes converts an hour of misattributed debugging into one sentence naming the
directories to delete.

WHAT THE FIRST VERSION GOT WRONG
================================
It blamed Cargo. Reclaiming 28 GiB of `target/` was what let the session resume, so `target/`
became the headline and the Bazel output user root became a single line item. A direct
measurement of the same host inverts that ranking by an order of magnitude.
`ci/bazel/README.md` carries the full table; the shape of it is:

  * Bazel keys the output base on the *workspace path*. Thirty-five agent worktrees and a
    drawer of `/private/tmp/mindclade-*` scratch clones are thirty-five-plus distinct
    workspaces, so they are thirty-five-plus distinct output bases.
  * Nothing collects them. `--experimental_disk_cache_gc_max_size` bounds the `--disk_cache`,
    and `ci/common/bazel_disk_cache.py` sets it to 1 GiB. It has no effect whatsoever on
    output bases, which have no size ceiling, no LRU, and no expiry.
  * When a workspace is deleted, its output base is not. It becomes unreachable garbage that
    only an explicit `rm -rf` will ever remove. On the measured host, fourteen of the thirty
    bases in the older output root pointed at workspaces that no longer existed, and those
    fourteen alone held more than seventy gigabytes.

So this module had two defects, both of which mattered on the host it was written for:

  1. It resolved exactly one output user root, and on macOS it resolved the wrong one. Bazel's
     macOS default moved from `/var/tmp/_bazel_$USER` to `~/Library/Caches/bazel/_bazel_$USER`.
     A host that has run more than one Bazel release has *both*, holding different bases, and a
     check that knows about one of them reports the other one's contents as the whole problem.
  2. It named the output root as one directory and tried to size it inside a two-second budget.
     A root holding a hundred gigabytes across half a million files reports "at least 0.4 GiB"
     under that budget, which is not so much an understatement as a misdirection.

WHY IT IS A PREFLIGHT AND NOT A CLEANUP
=======================================
This module never deletes anything. Reclaim candidates on a shared host belong to other
workers -- a sibling worktree's `target/` is another agent's warm build cache, and a live
output base is expensive to rebuild. Naming them and refusing to start is correct; removing
them behind the operator's back is not.

The one category this module is willing to be unambiguous about is an *orphaned* output base:
one whose `DO_NOT_BUILD_HERE` names a workspace directory that is not there any more. Nothing
can ever read that base again. It still does not delete it -- it says which ones they are, so
the operator's `rm -rf` is aimed rather than speculative.

THRESHOLD DERIVATION
====================
`DEFAULT_MIN_FREE_BYTES` is the measured cost of one full run on one workspace, rounded up:

  Bazel output base for one workspace  4.6 GiB   measured: live bases on the reference host
                                                 range from 0.01 GiB (analysis only) to
                                                 8.7 GiB (a full `//...` test run). The floor
                                                 has to cover the expensive end, because that
                                                 is the run this gate stands in front of.
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

Note what the floor deliberately does *not* claim to be: a bound on the total. One run's
consumption is bounded and measurable. The accumulated total across every workspace that has
ever existed on the host is not bounded by anything Bazel offers, and presenting a free-space
floor as if it bounded that would be the same category error as the 28 GiB headline.

RELATIONSHIP TO `ci/common/prepare_nix_runner.sh`
=================================================
That script already asserts 50 GiB free on `/` -- but it refuses to run anywhere except an
ephemeral GitHub-hosted Linux runner, and it looks only at `/`. The outage this module was
written for happened on a developer host, on the data volume, with the Bazel output user roots
and thirty-five worktrees' `target/` directories on it. This check runs wherever the pipeline
runs and probes every filesystem the lanes actually write to. The 50 GiB hosted floor
comfortably satisfies the floor here, so the two do not contradict each other.

The number is deliberately a single reviewed constant rather than a percentage. A percentage of
a 460 GiB volume and a percentage of a 40 GiB runner disk are not the same policy, and the
quantity that matters is absolute: how many bytes the next hour of work is going to write.
"""

from __future__ import annotations

import argparse
import math
import os
import shutil
import sys
import time
from collections.abc import Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]

GIB = 1024**3

# See THRESHOLD DERIVATION above. Raising this is a policy change; lowering it to make a
# constrained host pass is the class of edit this file exists to prevent.
DEFAULT_MIN_FREE_BYTES = 16 * GIB

# Wall-clock ceiling for any single directory in the diagnostic sizing walk. The check itself is
# a `statvfs` and costs microseconds; only the failure path measures reclaim candidates, and it
# must not turn an out-of-disk abort into a hang on a filesystem that is already struggling.
SIZE_SCAN_SECONDS = 2.0

# Wall-clock ceiling for the sizing walk as a whole, and the floor each candidate keeps no
# matter how many of them there are.
#
# WHY BOTH. A purely per-candidate budget is unbounded in the number of candidates, and the
# number of candidates is exactly what grows on the host this gate exists for: the reference
# machine has thirty-five sibling worktrees, so a 2s-per-candidate walk is over a minute of
# diagnostics bolted onto an abort. A purely global budget fails in the other direction -- it
# is spent entirely by whichever tree is scanned first, and every candidate after it reports
# "0.0 GiB", which reads as "these are empty" rather than "these were not measured".
#
# So each candidate receives an equal share of whatever remains, clamped into
# [SIZE_SCAN_MIN_SECONDS, SIZE_SCAN_SECONDS]. Candidates that finish early return their unspent
# time to the ones after them, a short list still gets the full per-directory budget, and a long
# list degrades into many honest lower bounds instead of one accurate number followed by a
# column of zeroes.
SIZE_SCAN_TOTAL_SECONDS = 8.0
SIZE_SCAN_MIN_SECONDS = 0.05

# Bazel writes the absolute workspace path into this file at the root of every output base, with
# no trailing newline. It is the only cheap, authoritative way to ask "which checkout does this
# base belong to, and is that checkout still there" -- one stat and one short read per base,
# rather than a tree walk per base.
WORKSPACE_MARKER = "DO_NOT_BUILD_HERE"

# Direct children of an output user root that are not output bases. `install` holds unpacked
# Bazel server binaries keyed by install hash; `cache` is the shared repository/download cache.
# Neither carries a `DO_NOT_BUILD_HERE`, and neither is per-workspace, so they are reported on
# their own terms rather than counted as bases.
NON_BASE_ENTRIES = ("cache", "install")


@dataclass(frozen=True)
class Candidate:
    """A directory an operator can delete to reclaim space, with why it is safe to consider."""

    path: Path
    what: str


@dataclass(frozen=True)
class OutputBase:
    """One Bazel output base, and whether the workspace that owns it still exists.

    `workspace` is `None` when the marker file is empty or unreadable. That is reported as
    "unknown" rather than folded into either bucket: calling a base orphaned when the evidence
    could not be read would invite an `rm -rf` of a live tree.
    """

    path: Path
    root: Path
    workspace: Path | None
    workspace_exists: bool

    @property
    def orphaned(self) -> bool:
        return self.workspace is not None and not self.workspace_exists

    @property
    def state(self) -> str:
        if self.workspace is None:
            return "unknown"
        return "live" if self.workspace_exists else "ORPHAN"


@dataclass(frozen=True)
class Mount:
    """One distinct filesystem that the expensive lanes write to."""

    device: int
    probe: Path
    free_bytes: int
    total_bytes: int


def bazel_output_user_roots() -> list[Path]:
    """Every output user root Bazel may have written on this host, in probe order.

    Deliberately plural, and that is the correction this function exists for. Bazel's macOS
    default output user root moved from `/var/tmp/_bazel_$USER` to
    `~/Library/Caches/bazel/_bazel_$USER`; the reference host has both, because it has run both
    releases. The older root held the larger pile and the newer root held every base created
    that day, so a check that resolves one of them is not merely incomplete -- it presents one
    directory as the whole problem, and the operator either deletes the wrong pile or deletes
    nothing because the number looked small.

    `bazel info output_base` would answer authoritatively for the *current* invocation and for
    nothing else, and it would answer by starting a Bazel server, which is precisely the
    expensive work this preflight is supposed to gate.
    """
    roots: list[Path] = []
    seen: set[Path] = set()

    def add(path: Path | None) -> None:
        # Deduplicated by RESOLVED path, not by spelling. `/var/tmp` is a symlink to
        # `/private/var/tmp` on macOS, so probing both spellings -- which this function does on
        # purpose, since the symlink is a platform convention rather than a guarantee -- would
        # otherwise count each root's shared `cache/` and `install/` twice and overstate the
        # total by several gigabytes. Bases are deduplicated separately, in `bazel_output_bases`.
        #
        # The resolved form is the deduplication KEY only; the list keeps the spelling that was
        # derived, because that is what the operator will type. Resolving for display is
        # actively unhelpful on macOS, where `/home/x` resolves to `/System/Volumes/Data/home/x`.
        if path is None:
            return
        try:
            key = path.resolve()
        except OSError:
            key = path
        if key in seen:
            return
        seen.add(key)
        roots.append(path)

    # Bazel honours TEST_TMPDIR ahead of every default, so a nested Bazel invocation inside a
    # test writes here and nowhere else.
    override = os.environ.get("TEST_TMPDIR")
    if override:
        add(Path(override))
    user = os.environ.get("USER") or os.environ.get("LOGNAME")
    if not user:
        return roots
    home = os.environ.get("HOME")
    cache_home = os.environ.get("XDG_CACHE_HOME")
    if cache_home:
        add(Path(cache_home) / "bazel" / f"_bazel_{user}")
    if sys.platform == "darwin":
        if home:
            add(Path(home) / "Library" / "Caches" / "bazel" / f"_bazel_{user}")
        # `/var` is a symlink to `/private/var` on macOS. Both spellings are probed because the
        # enumeration resolves each base before deduplicating, so the second spelling costs one
        # `listdir` and removes a whole class of "the check looked in the wrong place".
        add(Path("/private/var/tmp") / f"_bazel_{user}")
        add(Path("/var/tmp") / f"_bazel_{user}")
    else:
        if home and not cache_home:
            add(Path(home) / ".cache" / "bazel" / f"_bazel_{user}")
        add(Path("/var/tmp") / f"_bazel_{user}")
    return roots


def bazel_output_bases(roots: Iterable[Path]) -> list[OutputBase]:
    """Enumerate output bases under `roots`, resolving each one's owning workspace.

    Costs one `listdir` per root plus one stat and one short read per base. It deliberately does
    not descend into any base: the structural facts -- how many bases exist, and which of them
    can never be read again -- are free, while their sizes are not.
    """
    bases: list[OutputBase] = []
    seen: set[Path] = set()
    for root in roots:
        try:
            entries = sorted(root.iterdir())
        except OSError:
            continue
        for entry in entries:
            if entry.name in NON_BASE_ENTRIES:
                continue
            try:
                if entry.is_symlink() or not entry.is_dir():
                    continue
                marker = entry / WORKSPACE_MARKER
                if not marker.is_file():
                    continue
                resolved = entry.resolve()
            except OSError:
                continue
            if resolved in seen:
                continue
            seen.add(resolved)
            try:
                raw = marker.read_text(encoding="utf-8", errors="replace").strip()
            except OSError:
                raw = ""
            workspace = Path(raw) if raw else None
            exists = False
            if workspace is not None:
                try:
                    exists = workspace.is_dir()
                except OSError:
                    exists = False
            bases.append(
                OutputBase(path=resolved, root=root, workspace=workspace, workspace_exists=exists)
            )
    return bases


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

    Sibling worktrees are the most numerous case: each one carries an independent multi-gigabyte
    `target/`, and the operator staring at a full disk usually has no idea how many of them
    exist. They are no longer the *largest* case -- see WHAT THE FIRST VERSION GOT WRONG -- but
    they remain the fastest thing to reclaim, because rebuilding one is minutes rather than the
    better part of an hour.
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


def _bazel_shared_caches(roots: Iterable[Path]) -> Iterator[Candidate]:
    """Yield the per-root directories that are shared rather than per-workspace.

    `cache/` is the repository and download cache, shared by every workspace under the root and
    as unbounded as the bases are; it was 3.9 GiB on the reference host, larger than any single
    live base there. Deleting it costs refetches rather than rebuilds, which makes it the
    cheapest multi-gigabyte reclaim on the list.
    """
    for root in roots:
        yield Candidate(root / "cache", f"Bazel repository/download cache under {root}")


def reclaim_candidates(repo: Path) -> list[Candidate]:
    """Every directory worth *sizing* in a failure message, deduplicated, existing only.

    Output bases are excluded on purpose. There are tens of them, each holding hundreds of
    thousands of files, and sizing them inside any budget an abort can afford produces lower
    bounds that are wrong by an order of magnitude. They are reported structurally instead --
    see `output_base_report` -- with `--report` available when exact numbers are wanted and
    minutes are affordable.
    """
    candidates: list[Candidate] = []
    seen: set[Path] = set()
    ordered: list[Candidate] = []
    ordered.extend(_bazel_shared_caches(bazel_output_user_roots()))
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


def size_candidates(
    candidates: list[Candidate], total_budget: float = SIZE_SCAN_TOTAL_SECONDS
) -> list[tuple[int, bool, Candidate]]:
    """Size every candidate under a shared budget, largest first.

    See SIZE_SCAN_TOTAL_SECONDS for why the budget is neither purely per-candidate nor purely
    global. Returns `(bytes, truncated, candidate)` tuples.
    """
    end = time.monotonic() + total_budget
    sized: list[tuple[int, bool, Candidate]] = []
    for index, candidate in enumerate(candidates):
        share = (end - time.monotonic()) / (len(candidates) - index)
        share = min(SIZE_SCAN_SECONDS, max(SIZE_SCAN_MIN_SECONDS, share))
        size, truncated = directory_size(candidate.path, time.monotonic() + share)
        sized.append((size, truncated, candidate))
    sized.sort(key=lambda item: item[0], reverse=True)
    return sized


def _format_bytes(value: int) -> str:
    return f"{value / GIB:.1f} GiB"


def output_base_report(bases: list[OutputBase], device: int | None = None) -> list[str]:
    """Render the structural facts about output bases: how many, and which are unreachable.

    Every fact here is exact and costs no tree walk, which is why it carries the message rather
    than a column of truncated sizes would. `device` restricts the report to one filesystem, so
    a per-mount failure does not list bases that live somewhere else.
    """
    selected = []
    for base in bases:
        if device is None:
            selected.append(base)
            continue
        try:
            if base.path.stat().st_dev == device:
                selected.append(base)
        except OSError:
            continue
    if not selected:
        return []
    orphaned = [base for base in selected if base.orphaned]
    roots = sorted({str(base.root) for base in selected})
    lines = [
        "",
        f"Bazel output bases on this filesystem: {len(selected)}, across "
        f"{len(roots)} output user root(s):",
    ]
    lines.extend(f"    {root}" for root in roots)
    lines.append(
        "  Bazel keys an output base on the workspace path, so every worktree and every scratch"
    )
    lines.append(
        "  clone gets its own, and nothing bounds their total. "
        "`--experimental_disk_cache_gc_max_size`"
    )
    lines.append(
        "  bounds the --disk_cache only; an output base has no ceiling, no LRU, and no expiry."
    )
    if orphaned:
        lines.append("")
        lines.append(
            f"  {len(orphaned)} of them belong to workspaces that no longer exist. Nothing can "
            "read these"
        )
        lines.append(
            "  again, so they are safe to remove, and only an explicit `rm -rf` ever will:"
        )
        for base in orphaned:
            lines.append(f"    {base.path}")
            lines.append(f"      workspace (gone): {base.workspace}")
    lines.append("")
    lines.append(
        "  Sizes are omitted here on purpose: walking tens of bases would add minutes to an"
    )
    lines.append("  abort. Measure them exactly with")
    lines.append("    python3 ci/presubmit/disk_preflight.py --report")
    lines.append("  and see ci/bazel/README.md for the reclamation procedure.")
    return lines


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
    bases = bazel_output_bases(bazel_output_user_roots())
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
        ]
        # Deliberately ahead of the sized table. The first version of this message put Cargo
        # first because Cargo was the only thing it could size inside its budget, and a reader
        # who acted on that reclaimed the 28 GiB while the 98 GiB of unreachable output bases
        # sat unmentioned. Ordering by "what the budget could measure" is not ordering by size.
        lines.extend(output_base_report(bases, mount.device))
        lines.append("")
        lines.append("Other reclaim candidates on this filesystem, largest first:")
        on_mount = []
        for candidate in candidates:
            try:
                if candidate.path.stat().st_dev != mount.device:
                    continue
            except OSError:
                continue
            on_mount.append(candidate)
        sized = size_candidates(on_mount)
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
                f"{SIZE_SCAN_TOTAL_SECONDS:g}s shared budget; the real total is larger."
            )
        failures.append("\n".join(lines))
    return failures


def report(repo: Path) -> int:
    """Measure the Bazel output roots exactly, with no time budget, and print the table.

    The deliberate counterpart to the failure path. An operator asking "where did the disk go"
    can afford minutes; an abort cannot. Same enumeration, no deadline, so these numbers are
    exact rather than lower bounds -- and they are the numbers `ci/bazel/README.md` quotes.
    """
    roots = bazel_output_user_roots()
    bases = bazel_output_bases(roots)
    no_deadline = math.inf
    grand_total = 0
    grand_orphan = 0
    print(f"repository: {repo}")
    print(f"Bazel output user roots probed ({len(roots)}):")
    for root in roots:
        print(f"  {root}" + ("" if root.is_dir() else "   (absent)"))
    for root in roots:
        if not root.is_dir():
            continue
        rows: list[tuple[int, str, str]] = []
        for base in bases:
            if base.root != root:
                continue
            size, _ = directory_size(base.path, no_deadline)
            rows.append((size, base.state, str(base.workspace or base.path)))
        for name in NON_BASE_ENTRIES:
            shared = root / name
            if shared.is_dir():
                size, _ = directory_size(shared, no_deadline)
                rows.append((size, "shared", str(shared)))
        rows.sort(reverse=True)
        root_total = sum(size for size, _, _ in rows)
        orphan_rows = [row for row in rows if row[1] == "ORPHAN"]
        orphan_total = sum(size for size, _, _ in orphan_rows)
        print("")
        print(f"== {root}")
        for size, state, label in rows:
            print(f"  {_format_bytes(size):>10}  {state:<7}  {label}")
        print(f"  {'-' * 68}")
        print(f"  {_format_bytes(root_total):>10}  total")
        print(
            f"  {_format_bytes(orphan_total):>10}  reclaimable now: {len(orphan_rows)} "
            "base(s) whose workspace no longer exists"
        )
        grand_total += root_total
        grand_orphan += orphan_total
    print("")
    print(
        f"{_format_bytes(grand_total)} total across {len(bases)} output base(s) "
        f"in {len(roots)} probed root(s); {_format_bytes(grand_orphan)} of it orphaned."
    )
    print("Reclamation procedure: ci/bazel/README.md")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--min-free-gib",
        type=float,
        default=DEFAULT_MIN_FREE_BYTES / GIB,
        help="Free-space floor per filesystem, in GiB. See THRESHOLD DERIVATION in this module.",
    )
    parser.add_argument("--repo", type=Path, default=REPO)
    parser.add_argument(
        "--report",
        action="store_true",
        help="Measure the Bazel output roots exactly and exit. Slow by design; no time budget.",
    )
    arguments = parser.parse_args(argv)
    if arguments.min_free_gib <= 0:
        parser.error("--min-free-gib must be positive; a zero floor is not a preflight")
    if arguments.report:
        return report(arguments.repo)
    failures = check(arguments.repo, int(arguments.min_free_gib * GIB))
    for failure in failures:
        print(failure, file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
