# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Select the cache-safe Bazel worker topology for one workflow event."""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from ci.common import full_graph_shards  # noqa: E402

PRESUBMIT_WORKER = -1
UNSHARDED_FULL_WORKER = -2


@dataclass(frozen=True)
class WorkerMatrix:
    workers: tuple[int, ...]
    mode: str
    shard_count: int

    def encoded_workers(self) -> str:
        return json.dumps(self.workers, separators=(",", ":"))


class WorkerMatrixError(RuntimeError):
    """A workflow event cannot be mapped to an authorized topology."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


def select(*, lane: str, event: str, remote_cache_enabled: bool, shard_count: int) -> WorkerMatrix:
    if type(shard_count) is not int or shard_count < 2:
        raise WorkerMatrixError(
            "BAZEL_MATRIX_INVALID_SHARD_COUNT",
            "the full-graph contract must declare at least two shards",
        )
    if lane == "presubmit":
        if event == "pull_request":
            return WorkerMatrix((PRESUBMIT_WORKER,), "presubmit-auto", shard_count)
        if event not in {"merge_group", "push"}:
            raise WorkerMatrixError(
                "BAZEL_MATRIX_UNEXPECTED_EVENT",
                "the presubmit worker matrix does not recognize this workflow event",
            )
    elif lane == "nightly":
        if event not in {"schedule", "workflow_dispatch"}:
            raise WorkerMatrixError(
                "BAZEL_MATRIX_UNEXPECTED_EVENT",
                "the nightly worker matrix does not recognize this workflow event",
            )
    else:
        raise WorkerMatrixError(
            "BAZEL_MATRIX_INVALID_LANE",
            "the requested Bazel worker lane is unsupported",
        )
    if remote_cache_enabled:
        return WorkerMatrix(tuple(range(shard_count)), "full-sharded", shard_count)
    return WorkerMatrix((UNSHARDED_FULL_WORKER,), "full-unsharded", shard_count)


def _strict_boolean(value: str) -> bool:
    if value == "true":
        return True
    if value == "false":
        return False
    raise WorkerMatrixError(
        "BAZEL_MATRIX_INVALID_CACHE_STATE",
        "the remote-cache selector must report exactly true or false",
    )


def _append_outputs(path: Path, matrix: WorkerMatrix) -> None:
    if path.is_symlink():
        raise WorkerMatrixError(
            "BAZEL_MATRIX_UNSAFE_OUTPUT",
            "the GitHub output boundary must not be a symbolic link",
        )
    try:
        with path.open("a", encoding="utf-8") as output:
            output.write(f"workers={matrix.encoded_workers()}\n")
            output.write(f"mode={matrix.mode}\n")
            output.write(f"shard_count={matrix.shard_count}\n")
    except OSError as error:
        raise WorkerMatrixError(
            "BAZEL_MATRIX_OUTPUT_FAILED",
            "the GitHub output boundary could not be updated",
        ) from error


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--lane", choices=("presubmit", "nightly"), required=True)
    parser.add_argument("--event", required=True)
    parser.add_argument("--remote-cache-enabled", required=True)
    parser.add_argument("--contract", type=Path, default=full_graph_shards.DEFAULT_CONTRACT)
    parser.add_argument("--github-output", type=Path)
    args = parser.parse_args()
    try:
        contract = full_graph_shards.load_contract(args.contract)
        matrix = select(
            lane=args.lane,
            event=args.event,
            remote_cache_enabled=_strict_boolean(args.remote_cache_enabled),
            shard_count=contract.shard_count,
        )
        if args.github_output is not None:
            _append_outputs(args.github_output, matrix)
        else:
            print(matrix.encoded_workers())
    except (WorkerMatrixError, full_graph_shards.ShardContractError) as error:
        code = getattr(error, "code", "BAZEL_MATRIX_CONTRACT_INVALID")
        print(f"{code}: Bazel worker topology selection failed", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
