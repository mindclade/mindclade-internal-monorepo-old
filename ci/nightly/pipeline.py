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
    try:
        if path.is_symlink():
            raise OSError("symbolic link")
        lines = [
            line
            for line in path.read_text(encoding="utf-8").splitlines()
            if line.strip() and not line.lstrip().startswith("#") and line.strip() != "---"
        ]
    except (OSError, UnicodeError) as error:
        raise affected.SelectionError(
            "AFFECTED-SELECT-017", "nightly target contract is unreadable"
        ) from error

    def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise affected.SelectionError(
                    "AFFECTED-SELECT-018", "nightly target contract is invalid"
                )
            result[key] = value
        return result

    def reject_constant(_value: str) -> None:
        raise affected.SelectionError("AFFECTED-SELECT-018", "nightly target contract is invalid")

    try:
        payload = json.loads(
            "\n".join(lines),
            object_pairs_hook=unique_object,
            parse_constant=reject_constant,
        )
        if not isinstance(payload, dict):
            raise ValueError("root")
        return NightlyContract.from_dict(payload)
    except affected.SelectionError:
        raise
    except (json.JSONDecodeError, RecursionError, ValueError) as error:
        raise affected.SelectionError(
            "AFFECTED-SELECT-018", "nightly target contract is invalid"
        ) from error


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--targets", type=Path, default=Path(__file__).with_name("targets.yaml"))
    parser.add_argument("--evidence-dir", type=Path)
    parser.add_argument("--job-started-at-file", type=Path)
    parser.add_argument("--runner-temp", type=Path)
    parser.add_argument("--event", default=os.environ.get("GITHUB_EVENT_NAME", "schedule"))
    parser.add_argument("--ref", default=os.environ.get("GITHUB_REF"))
    parser.add_argument("--head", default=os.environ.get("GITHUB_SHA"))
    parser.add_argument("--cache-mode", choices=("disk", "remote"))
    parser.add_argument("--cache-role", choices=("reader", "writer"))
    args = parser.parse_args()
    evidence_dir = args.evidence_dir or Path(tempfile.mkdtemp(prefix="mindclade-nightly-"))
    try:
        contract = load_contract(args.targets)
        mode = affected.resolve_selection_mode(
            contract.mode,
            event=args.event,
            ref=args.ref,
            base_sha=None,
        )
        if args.cache_mode is None or args.cache_role is None:
            raise affected.SelectionError(
                "AFFECTED-SELECT-020", "Bazel runtime contract is invalid"
            )
        if not args.head:
            raise affected.SelectionError(
                "AFFECTED-SELECT-019", "checkout integrity validation failed"
            )
        if args.runner_temp is None:
            raise affected.SelectionError(
                "AFFECTED-SELECT-020", "Bazel runtime contract is invalid"
            )
        if args.job_started_at_file is None:
            raise affected.SelectionError("AFFECTED-SELECT-014", "job-start timestamp is invalid")
        bazelrc_authority = affected.assert_clean_checkout(
            args.head,
            event=args.event,
            runner_temp=args.runner_temp,
            cache_mode=args.cache_mode,
            cache_role=args.cache_role,
        )
        selection = affected.select([], mode=mode, event=args.event)
        if selection.analysis_targets != contract.analysis_targets:
            raise affected.SelectionError(
                "AFFECTED-SELECT-015", "nightly analysis target contract drifted"
            )
        if selection.test_targets != contract.test_targets:
            raise affected.SelectionError(
                "AFFECTED-SELECT-015", "nightly test target contract drifted"
            )
        return affected.execute_selection(
            selection,
            evidence_dir,
            bazelrc_authority=bazelrc_authority,
            job_started_epoch=affected.load_job_started_epoch(
                args.job_started_at_file,
                runner_temp=args.runner_temp,
            ),
        )
    except affected.SelectionError as error:
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
