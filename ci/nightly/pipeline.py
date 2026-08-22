# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""CPU nightly full-graph Bazel qualification."""

from __future__ import annotations

import argparse
import json
import os
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from ci.common import affected  # noqa: E402


@dataclass(frozen=True)
class NightlyContract:
    mode: str
    analysis_targets: tuple[str, ...]
    test_targets: tuple[str, ...]

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> NightlyContract:
        expected = {"schema_version", "mode", "analysis_targets", "test_targets"}
        unknown = set(payload) - expected
        if unknown:
            raise ValueError(f"unknown nightly contract fields: {sorted(unknown)}")
        if type(payload.get("schema_version")) is not int or payload["schema_version"] != 1:
            raise ValueError("nightly schema_version must be integer 1")
        if payload.get("mode") != "full":
            raise ValueError("nightly mode must be full")
        for field in ("analysis_targets", "test_targets"):
            value = payload.get(field)
            if (
                not isinstance(value, list)
                or not value
                or not all(isinstance(target, str) and target.startswith("//") for target in value)
            ):
                raise ValueError(f"nightly {field} must be a non-empty Bazel target list")
        return cls(
            mode="full",
            analysis_targets=tuple(payload["analysis_targets"]),
            test_targets=tuple(payload["test_targets"]),
        )


def load_contract(path: Path) -> NightlyContract:
    if path.is_symlink():
        raise ValueError(f"nightly contract must not be a symbolic link: {path}")
    lines = [
        line
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.lstrip().startswith("#") and line.strip() != "---"
    ]
    return NightlyContract.from_dict(json.loads("\n".join(lines)))


def _job_started_epoch(path: Path | None) -> float | None:
    if path is None:
        return None
    try:
        return float(path.read_text(encoding="utf-8").strip())
    except (OSError, ValueError) as error:
        raise affected.SelectionError(f"invalid job-start timestamp {path}: {error}") from error


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--targets", type=Path, default=Path(__file__).with_name("targets.yaml"))
    parser.add_argument("--evidence-dir", type=Path)
    parser.add_argument("--job-started-at-file", type=Path)
    parser.add_argument("--event", default=os.environ.get("GITHUB_EVENT_NAME", "schedule"))
    args = parser.parse_args()
    evidence_dir = args.evidence_dir or Path(tempfile.mkdtemp(prefix="mindclade-nightly-"))
    try:
        contract = load_contract(args.targets)
        selection = affected.select([], mode=contract.mode, event=args.event)
        if selection.analysis_targets != contract.analysis_targets:
            raise affected.SelectionError("nightly analysis target contract drift")
        if selection.test_targets != contract.test_targets:
            raise affected.SelectionError("nightly test target contract drift")
        return affected.execute_selection(
            selection,
            evidence_dir,
            job_started_epoch=_job_started_epoch(args.job_started_at_file),
        )
    except (affected.SelectionError, ValueError, json.JSONDecodeError) as error:
        print(f"nightly Bazel qualification failed: {error}", file=sys.stderr)
        affected.write_failure_evidence(
            evidence_dir,
            mode="full",
            event=args.event,
            base_sha=None,
            error=error,
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
