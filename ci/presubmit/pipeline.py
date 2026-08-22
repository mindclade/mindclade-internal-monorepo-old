# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Presubmit orchestration for static policy and affected Bazel validation."""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from ci.common import affected  # noqa: E402


def run(command: list[str]) -> int:
    return subprocess.call(command, cwd=REPO)


def _job_started_epoch(path: Path | None) -> float | None:
    if path is None:
        return None
    try:
        return float(path.read_text(encoding="utf-8").strip())
    except (OSError, ValueError) as error:
        raise affected.SelectionError(
            "AFFECTED-SELECT-014", "job-start timestamp is invalid"
        ) from error


def main() -> int:
    parser = argparse.ArgumentParser()
    phase = parser.add_mutually_exclusive_group()
    phase.add_argument("--static-only", action="store_true")
    phase.add_argument("--bazel-only", action="store_true")
    parser.add_argument("--mode", choices=("auto", "affected", "full"), default="auto")
    parser.add_argument("--base")
    parser.add_argument("--event", default=os.environ.get("GITHUB_EVENT_NAME", "local"))
    parser.add_argument("--ref", default=os.environ.get("GITHUB_REF"))
    parser.add_argument("--head", default=os.environ.get("GITHUB_SHA"))
    parser.add_argument("--evidence-dir", type=Path)
    parser.add_argument("--job-started-at-file", type=Path)
    args = parser.parse_args()

    if not args.bazel_only and run(
        [sys.executable, str(REPO / "tools/analysis/run_architecture_checks.py")]
    ):
        return 1
    if args.static_only:
        return 0

    evidence_dir = args.evidence_dir or Path(tempfile.mkdtemp(prefix="mindclade-bazel-"))
    base_sha: str | None = None
    resolved_mode = args.mode
    try:
        resolved_mode = affected.resolve_selection_mode(
            args.mode,
            event=args.event,
            ref=args.ref,
            base_sha=args.base,
        )
        if args.event != "local":
            if not args.head:
                raise affected.SelectionError(
                    "AFFECTED-SELECT-019", "checkout integrity validation failed"
                )
            affected.assert_clean_checkout(args.head)
            affected.assert_bazelrc_contract(args.event)
        if resolved_mode == "affected":
            if not args.base:
                raise affected.SelectionError(
                    "AFFECTED-SELECT-011",
                    "affected selection requires an explicit base revision",
                )
            base_sha = affected.git_revision(args.base)
            changes = affected.git_changed(base_sha)
        else:
            changes = ()

        if not args.bazel_only:
            if affected.rust_qualification_required(changes):
                if run(
                    [
                        sys.executable,
                        str(REPO / "tools/qualification/rust/qualify.py"),
                        "--mode",
                        "presubmit",
                    ]
                ):
                    return 1
            else:
                print("Skipping full Rust qualification: no Rust/runtime/toolchain inputs changed")

        selection = affected.select(
            changes,
            mode=resolved_mode,
            base_sha=base_sha,
            event=args.event,
        )
        print(
            f"Bazel selection: mode={selection.mode} reason={selection.reason} "
            f"analysis={len(selection.analysis_targets)} tests={len(selection.test_targets)}"
        )
        return affected.execute_selection(
            selection,
            evidence_dir,
            job_started_epoch=_job_started_epoch(args.job_started_at_file),
        )
    except affected.SelectionError as error:
        print(f"affected Bazel selection failed: {error}", file=sys.stderr)
        affected.write_failure_evidence(
            evidence_dir,
            mode=resolved_mode,
            event=args.event,
            base_sha=base_sha,
            error=error,
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
