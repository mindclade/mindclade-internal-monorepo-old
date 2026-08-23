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
from pathlib import Path, PurePosixPath
from typing import Any

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from ci.common import affected, full_graph_shards  # noqa: E402


@dataclass(frozen=True)
class NightlyContract:
    mode: str
    shard_count: int
    partition_contract: str

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> NightlyContract:
        expected = {"schema_version", "mode", "shard_count", "partition_contract"}
        unknown = set(payload) - expected
        missing = expected - set(payload)
        if unknown:
            raise ValueError(f"unknown nightly contract fields: {sorted(unknown)}")
        if missing:
            raise ValueError(f"missing nightly contract fields: {sorted(missing)}")
        if type(payload.get("schema_version")) is not int or payload["schema_version"] != 2:
            raise ValueError("nightly schema_version must be integer 2")
        if payload.get("mode") != "full":
            raise ValueError("nightly mode must be full")
        shard_count = payload.get("shard_count")
        if type(shard_count) is not int or shard_count < 2:
            raise ValueError("nightly shard_count must be an integer >= 2")
        partition_contract = payload.get("partition_contract")
        if not isinstance(partition_contract, str):
            raise ValueError("nightly partition_contract must be a repository-relative path")
        relative = PurePosixPath(partition_contract)
        if (
            relative.is_absolute()
            or ".." in relative.parts
            or relative.as_posix() != partition_contract
            or relative.parts[:2] != ("ci", "bazel")
            or len(relative.parts) < 3
            or relative.suffix != ".toml"
        ):
            raise ValueError("nightly partition_contract must remain under ci/bazel")
        return cls(
            mode="full",
            shard_count=shard_count,
            partition_contract=partition_contract,
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
    parser.add_argument("--shard-index", type=int)
    parser.add_argument("--shard-count", type=int)
    args = parser.parse_args()
    shard_arguments = (args.shard_index is not None, args.shard_count is not None)
    if any(shard_arguments) and not all(shard_arguments):
        parser.error("full-graph sharding requires both --shard-index and --shard-count")
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
        if args.shard_index is None or args.shard_count is None:
            selection = affected.select([], mode=mode, event=args.event)
        else:
            try:
                if args.shard_count != contract.shard_count:
                    raise full_graph_shards.ShardContractError(
                        "runtime shard count does not match the nightly contract"
                    )
                partition_path = REPO / contract.partition_contract
                shard_contract = full_graph_shards.load_contract(partition_path)
                if shard_contract.shard_count != contract.shard_count:
                    raise full_graph_shards.ShardContractError(
                        "nightly and full-graph shard contracts disagree"
                    )
                graph = full_graph_shards.plan_from_bazel(shard_contract)
                selection = full_graph_shards.selection_for_shard(
                    graph,
                    args.shard_index,
                    event=args.event,
                    head_sha=affected.git_revision("HEAD"),
                )
            except full_graph_shards.ShardContractError as error:
                raise affected.SelectionError(
                    "AFFECTED-SELECT-023", "full-graph shard contract is invalid"
                ) from error
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
