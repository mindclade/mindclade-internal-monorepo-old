# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Stable, redacted verdict over a cache-safe Bazel worker matrix."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

RESULTS = frozenset({"success", "failure", "cancelled", "skipped"})
TOPOLOGY_MODES = frozenset({"presubmit-auto", "full-unsharded", "full-sharded"})
SELECTION_MODES = frozenset({"affected", "full"})
WORKER_SELECTION_SCHEMA_VERSION = 1
PARTITION_SCHEMA_VERSION = 2
WORKER_SELECTION_FILENAME = "worker-selection.json"
MAX_SELECTION_BYTES = 4 * 1024 * 1024
SHA1_PATTERN = re.compile(r"[0-9a-f]{40}")
SHA256_PATTERN = re.compile(r"[0-9a-f]{64}")
ARTIFACT_PREFIX_PATTERN = re.compile(r"bazel-selection-[1-9][0-9]*-[1-9][0-9]*-")


@dataclass(frozen=True)
class VerdictError:
    code: str
    message: str


class VerdictContractError(RuntimeError):
    """Worker evidence is absent, ambiguous, or inconsistent."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.public_message = message


@dataclass(frozen=True)
class WorkerSelectionEvidence:
    worker: int
    topology_mode: str
    event: str
    head_sha: str
    selection_mode: str
    shard_count: int
    contract_sha256: str | None
    analysis_graph_sha256: str | None
    test_graph_sha256: str | None
    partition_manifest_sha256: str | None
    selected_shard_index: int | None

    def as_dict(self) -> dict[str, Any]:
        return {
            "schema_version": WORKER_SELECTION_SCHEMA_VERSION,
            "worker": self.worker,
            "topology_mode": self.topology_mode,
            "event": self.event,
            "head_sha": self.head_sha,
            "selection_mode": self.selection_mode,
            "shard_count": self.shard_count,
            "contract_sha256": self.contract_sha256,
            "analysis_graph_sha256": self.analysis_graph_sha256,
            "test_graph_sha256": self.test_graph_sha256,
            "partition_manifest_sha256": self.partition_manifest_sha256,
            "selected_shard_index": self.selected_shard_index,
        }


def _error(code: str, message: str) -> VerdictContractError:
    return VerdictContractError(code, message)


def _require_mapping(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    return value


def _require_exact_keys(value: dict[str, Any], expected: set[str]) -> None:
    if set(value) != expected:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")


def _require_integer(value: Any, *, minimum: int = 0) -> int:
    if type(value) is not int or value < minimum:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    return value


def _require_sha256(value: Any) -> str:
    if not isinstance(value, str) or SHA256_PATTERN.fullmatch(value) is None:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    return value


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def _read_json(path: Path) -> dict[str, Any]:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise _error(
            "BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is unreadable"
        ) from error
    try:
        before = os.fstat(descriptor)
        if (
            not stat.S_ISREG(before.st_mode)
            or before.st_mode & (stat.S_IWGRP | stat.S_IWOTH)
            or before.st_size <= 0
            or before.st_size > MAX_SELECTION_BYTES
        ):
            raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
        chunks = []
        total = 0
        while True:
            chunk = os.read(descriptor, min(64 * 1024, MAX_SELECTION_BYTES + 1 - total))
            if not chunk:
                break
            chunks.append(chunk)
            total += len(chunk)
            if total > MAX_SELECTION_BYTES:
                raise _error(
                    "BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid"
                )
        payload = b"".join(chunks)
        after = os.fstat(descriptor)
        before_identity = (
            before.st_dev,
            before.st_ino,
            before.st_mode,
            before.st_size,
            before.st_mtime_ns,
        )
        after_identity = (
            after.st_dev,
            after.st_ino,
            after.st_mode,
            after.st_size,
            after.st_mtime_ns,
        )
        if before_identity != after_identity or len(payload) != before.st_size:
            raise _error(
                "BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence changed while read"
            )
    except OSError as error:
        raise _error(
            "BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is unreadable"
        ) from error
    finally:
        os.close(descriptor)
    try:
        decoded = json.loads(
            payload.decode("utf-8"),
            object_pairs_hook=_unique_object,
            parse_constant=lambda _value: (_ for _ in ()).throw(ValueError("constant")),
        )
    except (UnicodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
        raise _error(
            "BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid"
        ) from error
    return _require_mapping(decoded)


def _write_json(path: Path, payload: dict[str, Any], *, runner_temp: Path) -> None:
    try:
        root = runner_temp.resolve(strict=True)
    except OSError as error:
        raise _error(
            "BAZEL_VERDICT_SELECTION_OUTPUT_INVALID",
            "worker selection output boundary is invalid",
        ) from error
    if runner_temp.is_symlink() or not root.is_dir():
        raise _error(
            "BAZEL_VERDICT_SELECTION_OUTPUT_INVALID",
            "worker selection output boundary is invalid",
        )
    parent = root / "bazel-worker-selection"
    expected = parent / WORKER_SELECTION_FILENAME
    if path != expected:
        raise _error(
            "BAZEL_VERDICT_SELECTION_OUTPUT_INVALID",
            "worker selection output boundary is invalid",
        )
    try:
        parent.mkdir(mode=0o700)
        encoded = json.dumps(payload, indent=2, sort_keys=True).encode("utf-8") + b"\n"
        flags = (
            os.O_WRONLY
            | os.O_CREAT
            | os.O_EXCL
            | getattr(os, "O_CLOEXEC", 0)
            | getattr(os, "O_NOFOLLOW", 0)
        )
        descriptor = os.open(expected, flags, 0o600)
        try:
            view = memoryview(encoded)
            while view:
                written = os.write(descriptor, view)
                if written <= 0:
                    raise OSError("short write")
                view = view[written:]
        finally:
            os.close(descriptor)
    except OSError as error:
        raise _error(
            "BAZEL_VERDICT_SELECTION_OUTPUT_INVALID",
            "worker selection output boundary is invalid",
        ) from error


def _expected_workers(topology_mode: str, shard_count: int) -> tuple[int, ...]:
    if type(shard_count) is not int or shard_count < 2:
        raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
    if topology_mode == "presubmit-auto":
        return (-1,)
    if topology_mode == "full-unsharded":
        return (-2,)
    if topology_mode == "full-sharded":
        return tuple(range(shard_count))
    raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")


def _validate_topology_event(topology_mode: str, event: str) -> None:
    if topology_mode == "presubmit-auto":
        valid = event == "pull_request"
    else:
        valid = event in {"merge_group", "push", "schedule", "workflow_dispatch"}
    if not valid:
        raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")


def _validate_execution(selection: dict[str, Any]) -> None:
    execution = selection.get("execution")
    completed_at = selection.get("completed_at")
    if not isinstance(execution, list) or len(execution) != 2 or not isinstance(completed_at, str):
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection did not complete")
    phases = []
    for item in execution:
        phase = _require_mapping(item)
        if (
            phase.get("phase") not in {"analysis", "test"}
            or phase.get("status") not in {"passed", "skipped"}
            or type(phase.get("exit_code")) is not int
            or phase["exit_code"] != 0
        ):
            raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection did not complete")
        phases.append(phase["phase"])
    if phases != ["analysis", "test"]:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection did not complete")


def _partition_summary(
    value: Any, *, worker: int, shard_count: int
) -> tuple[str, str, str, str, int]:
    partition = _require_mapping(value)
    _require_exact_keys(
        partition,
        {
            "schema_version",
            "contract_sha256",
            "shard_count",
            "analysis_query",
            "analysis_target_count",
            "analysis_graph_sha256",
            "test_query",
            "test_target_count",
            "test_graph_sha256",
            "weighted_test_target_count",
            "shards",
            "selected_shard",
        },
    )
    if partition["schema_version"] != PARTITION_SCHEMA_VERSION:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker partition evidence is invalid")
    observed_shard_count = _require_integer(partition["shard_count"], minimum=2)
    if observed_shard_count != shard_count:
        raise _error("BAZEL_VERDICT_PARTITION_MISMATCH", "worker partition evidence disagrees")
    contract_sha256 = _require_sha256(partition["contract_sha256"])
    analysis_graph_sha256 = _require_sha256(partition["analysis_graph_sha256"])
    test_graph_sha256 = _require_sha256(partition["test_graph_sha256"])
    analysis_target_count = _require_integer(partition["analysis_target_count"])
    test_target_count = _require_integer(partition["test_target_count"])
    weighted_test_target_count = _require_integer(partition["weighted_test_target_count"])
    if weighted_test_target_count > test_target_count:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker partition evidence is invalid")
    if not isinstance(partition["analysis_query"], str) or not isinstance(
        partition["test_query"], str
    ):
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker partition evidence is invalid")
    shards = partition["shards"]
    if not isinstance(shards, list) or len(shards) != shard_count:
        raise _error("BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "shard coverage is incomplete")
    normalized_shards: list[dict[str, int | str]] = []
    for index, item in enumerate(shards):
        shard = _require_mapping(item)
        _require_exact_keys(
            shard,
            {
                "index",
                "analysis_target_count",
                "analysis_targets_sha256",
                "test_target_count",
                "test_targets_sha256",
                "estimated_test_duration_ms",
            },
        )
        normalized = {
            "index": _require_integer(shard["index"]),
            "analysis_target_count": _require_integer(shard["analysis_target_count"]),
            "analysis_targets_sha256": _require_sha256(shard["analysis_targets_sha256"]),
            "test_target_count": _require_integer(shard["test_target_count"]),
            "test_targets_sha256": _require_sha256(shard["test_targets_sha256"]),
            "estimated_test_duration_ms": _require_integer(shard["estimated_test_duration_ms"]),
        }
        if normalized["index"] != index:
            raise _error("BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "shard coverage is incomplete")
        normalized_shards.append(normalized)
    if sum(item["analysis_target_count"] for item in normalized_shards) != analysis_target_count:
        raise _error("BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "shard coverage is incomplete")
    if sum(item["test_target_count"] for item in normalized_shards) != test_target_count:
        raise _error("BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "shard coverage is incomplete")
    selected = _require_mapping(partition["selected_shard"])
    if selected != normalized_shards[worker]:
        raise _error(
            "BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "selected shard identity is invalid"
        )
    plan = {
        "schema_version": PARTITION_SCHEMA_VERSION,
        "contract_sha256": contract_sha256,
        "shard_count": shard_count,
        "analysis_query": partition["analysis_query"],
        "analysis_target_count": analysis_target_count,
        "analysis_graph_sha256": analysis_graph_sha256,
        "test_query": partition["test_query"],
        "test_target_count": test_target_count,
        "test_graph_sha256": test_graph_sha256,
        "weighted_test_target_count": weighted_test_target_count,
        "shards": normalized_shards,
    }
    encoded = json.dumps(plan, ensure_ascii=True, separators=(",", ":"), sort_keys=True).encode(
        "ascii"
    )
    return (
        contract_sha256,
        analysis_graph_sha256,
        test_graph_sha256,
        hashlib.sha256(encoded).hexdigest(),
        worker,
    )


def redact_selection(
    selection: dict[str, Any],
    *,
    worker: int,
    topology_mode: str,
    event: str,
    head_sha: str,
    shard_count: int,
) -> WorkerSelectionEvidence:
    expected_workers = _expected_workers(topology_mode, shard_count)
    _validate_topology_event(topology_mode, event)
    if worker not in expected_workers:
        raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
    if SHA1_PATTERN.fullmatch(head_sha) is None or selection.get("head_sha") != head_sha:
        raise _error("BAZEL_VERDICT_SELECTION_IDENTITY_MISMATCH", "worker identity disagrees")
    if selection.get("event") != event or selection.get("schema_version") != 1:
        raise _error("BAZEL_VERDICT_SELECTION_IDENTITY_MISMATCH", "worker identity disagrees")
    selection_mode = selection.get("mode")
    if selection_mode not in SELECTION_MODES:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    _validate_execution(selection)
    if topology_mode == "full-sharded":
        if selection_mode != "full":
            raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
        (
            contract_sha256,
            analysis_graph_sha256,
            test_graph_sha256,
            partition_manifest_sha256,
            selected_shard_index,
        ) = _partition_summary(selection.get("partition"), worker=worker, shard_count=shard_count)
    else:
        if topology_mode == "full-unsharded" and selection_mode != "full":
            raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
        if selection.get("partition") is not None:
            raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
        contract_sha256 = None
        analysis_graph_sha256 = None
        test_graph_sha256 = None
        partition_manifest_sha256 = None
        selected_shard_index = None
    return WorkerSelectionEvidence(
        worker=worker,
        topology_mode=topology_mode,
        event=event,
        head_sha=head_sha,
        selection_mode=selection_mode,
        shard_count=shard_count,
        contract_sha256=contract_sha256,
        analysis_graph_sha256=analysis_graph_sha256,
        test_graph_sha256=test_graph_sha256,
        partition_manifest_sha256=partition_manifest_sha256,
        selected_shard_index=selected_shard_index,
    )


def _load_worker_evidence(path: Path) -> WorkerSelectionEvidence:
    payload = _read_json(path)
    _require_exact_keys(
        payload,
        {
            "schema_version",
            "worker",
            "topology_mode",
            "event",
            "head_sha",
            "selection_mode",
            "shard_count",
            "contract_sha256",
            "analysis_graph_sha256",
            "test_graph_sha256",
            "partition_manifest_sha256",
            "selected_shard_index",
        },
    )
    if payload["schema_version"] != WORKER_SELECTION_SCHEMA_VERSION:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    worker = payload["worker"]
    if type(worker) is not int:
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    topology_mode = payload["topology_mode"]
    event = payload["event"]
    head_sha = payload["head_sha"]
    selection_mode = payload["selection_mode"]
    shard_count = _require_integer(payload["shard_count"], minimum=2)
    if (
        topology_mode not in TOPOLOGY_MODES
        or not isinstance(event, str)
        or not isinstance(head_sha, str)
        or SHA1_PATTERN.fullmatch(head_sha) is None
        or selection_mode not in SELECTION_MODES
    ):
        raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
    digest_fields = (
        payload["contract_sha256"],
        payload["analysis_graph_sha256"],
        payload["test_graph_sha256"],
        payload["partition_manifest_sha256"],
    )
    selected_shard_index = payload["selected_shard_index"]
    if topology_mode == "full-sharded":
        digests = tuple(_require_sha256(value) for value in digest_fields)
        selected_shard_index = _require_integer(selected_shard_index)
    else:
        if any(value is not None for value in (*digest_fields, selected_shard_index)):
            raise _error("BAZEL_VERDICT_SELECTION_INVALID", "worker selection evidence is invalid")
        digests = (None, None, None, None)
    return WorkerSelectionEvidence(
        worker=worker,
        topology_mode=topology_mode,
        event=event,
        head_sha=head_sha,
        selection_mode=selection_mode,
        shard_count=shard_count,
        contract_sha256=digests[0],
        analysis_graph_sha256=digests[1],
        test_graph_sha256=digests[2],
        partition_manifest_sha256=digests[3],
        selected_shard_index=selected_shard_index,
    )


def parse_expected_workers(value: str) -> tuple[int, ...]:
    try:
        decoded = json.loads(
            value, parse_constant=lambda _value: (_ for _ in ()).throw(ValueError())
        )
    except (json.JSONDecodeError, RecursionError, ValueError) as error:
        raise _error(
            "BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid"
        ) from error
    if (
        not isinstance(decoded, list)
        or not decoded
        or any(type(worker) is not int for worker in decoded)
        or len(decoded) != len(set(decoded))
    ):
        raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
    return tuple(decoded)


def verify_worker_selections(
    root: Path,
    *,
    artifact_prefix: str,
    expected_workers: tuple[int, ...],
    topology_mode: str,
    event: str,
    head_sha: str,
    shard_count: int,
) -> None:
    expected_topology = _expected_workers(topology_mode, shard_count)
    _validate_topology_event(topology_mode, event)
    if expected_workers != expected_topology or SHA1_PATTERN.fullmatch(head_sha) is None:
        raise _error("BAZEL_VERDICT_TOPOLOGY_INVALID", "Bazel worker topology is invalid")
    if ARTIFACT_PREFIX_PATTERN.fullmatch(artifact_prefix) is None:
        raise _error("BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is invalid")
    if root.is_symlink() or not root.is_dir():
        raise _error("BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is unavailable")
    expected_names = {f"{artifact_prefix}{worker}": worker for worker in expected_workers}
    try:
        entries = tuple(root.iterdir())
    except OSError as error:
        raise _error(
            "BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is unavailable"
        ) from error
    if {entry.name for entry in entries} != set(expected_names):
        raise _error("BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is incomplete")
    evidence = []
    for entry in entries:
        if entry.is_symlink() or not entry.is_dir():
            raise _error("BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is invalid")
        try:
            children = tuple(entry.iterdir())
        except OSError as error:
            raise _error(
                "BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is invalid"
            ) from error
        if len(children) != 1 or children[0].name != WORKER_SELECTION_FILENAME:
            raise _error("BAZEL_VERDICT_ARTIFACT_SET_INVALID", "worker artifact set is invalid")
        item = _load_worker_evidence(children[0])
        expected_worker = expected_names[entry.name]
        if (
            item.worker != expected_worker
            or item.topology_mode != topology_mode
            or item.event != event
            or item.head_sha != head_sha
            or item.shard_count != shard_count
            or (
                topology_mode in {"full-unsharded", "full-sharded"}
                and item.selection_mode != "full"
            )
            or (topology_mode == "full-sharded" and item.selected_shard_index != expected_worker)
        ):
            raise _error("BAZEL_VERDICT_SELECTION_IDENTITY_MISMATCH", "worker identity disagrees")
        evidence.append(item)
    if topology_mode == "full-sharded":
        consensus = {
            (
                item.contract_sha256,
                item.analysis_graph_sha256,
                item.test_graph_sha256,
                item.partition_manifest_sha256,
            )
            for item in evidence
        }
        if len(consensus) != 1:
            raise _error("BAZEL_VERDICT_PARTITION_MISMATCH", "worker partition evidence disagrees")
        indexes = {item.selected_shard_index for item in evidence}
        if indexes != set(range(shard_count)):
            raise _error("BAZEL_VERDICT_PARTITION_COVERAGE_INVALID", "shard coverage is incomplete")


def evaluate(*, lane: str, event: str, plan: str, workers: str) -> tuple[VerdictError, ...]:
    values = {"plan": plan, "workers": workers}
    invalid = sorted(name for name, result in values.items() if result not in RESULTS)
    if invalid:
        return (
            VerdictError(
                "BAZEL_VERDICT_INVALID_RESULT",
                "one or more prerequisite jobs reported an unsupported result",
            ),
        )
    if lane == "presubmit":
        if event not in {"pull_request", "merge_group", "push"}:
            return (
                VerdictError(
                    "BAZEL_VERDICT_UNEXPECTED_EVENT",
                    "the presubmit verdict does not recognize this workflow event",
                ),
            )
    elif lane == "nightly":
        if event not in {"schedule", "workflow_dispatch"}:
            return (
                VerdictError(
                    "BAZEL_VERDICT_UNEXPECTED_EVENT",
                    "the nightly verdict does not recognize this workflow event",
                ),
            )
    else:
        return (
            VerdictError(
                "BAZEL_VERDICT_INVALID_LANE", "the requested Bazel verdict lane is unsupported"
            ),
        )

    errors = []
    for name, result in values.items():
        if result != "success":
            errors.append(
                VerdictError(
                    f"BAZEL_VERDICT_{name.upper()}_SUCCESS_REQUIRED",
                    f"the {name} prerequisite must report success for this lane",
                )
            )
    return tuple(errors)


def _redact_command(args: argparse.Namespace) -> int:
    try:
        evidence = redact_selection(
            _read_json(args.source),
            worker=args.worker,
            topology_mode=args.topology_mode,
            event=args.event,
            head_sha=args.head_sha,
            shard_count=args.shard_count,
        )
        _write_json(args.output, evidence.as_dict(), runner_temp=args.runner_temp)
    except VerdictContractError as error:
        print(f"{error.code}: {error.public_message}", file=sys.stderr)
        return 2
    return 0


def _verify_command(args: argparse.Namespace) -> int:
    errors = list(
        evaluate(
            lane=args.lane,
            event=args.event,
            plan=args.plan_result,
            workers=args.workers_result,
        )
    )
    if not errors:
        try:
            verify_worker_selections(
                args.selection_root,
                artifact_prefix=args.artifact_prefix,
                expected_workers=parse_expected_workers(args.expected_workers),
                topology_mode=args.topology_mode,
                event=args.event,
                head_sha=args.head_sha,
                shard_count=args.shard_count,
            )
        except VerdictContractError as error:
            errors.append(VerdictError(error.code, error.public_message))
    if errors:
        for error in errors:
            print(f"{error.code}: {error.message}", file=sys.stderr)
        return 1
    print(f"BAZEL_VERDICT_OK: {args.lane} qualification is complete")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    redact = commands.add_parser("redact-selection")
    redact.add_argument("--source", type=Path, required=True)
    redact.add_argument("--output", type=Path, required=True)
    redact.add_argument("--runner-temp", type=Path, required=True)
    redact.add_argument("--worker", type=int, required=True)
    redact.add_argument("--topology-mode", choices=tuple(sorted(TOPOLOGY_MODES)), required=True)
    redact.add_argument("--event", required=True)
    redact.add_argument("--head-sha", required=True)
    redact.add_argument("--shard-count", type=int, required=True)
    redact.set_defaults(handler=_redact_command)

    verify = commands.add_parser("verify")
    verify.add_argument("--lane", choices=("presubmit", "nightly"), required=True)
    verify.add_argument("--event", required=True)
    verify.add_argument("--head-sha", required=True)
    verify.add_argument("--plan-result", required=True)
    verify.add_argument("--workers-result", required=True)
    verify.add_argument("--topology-mode", required=True)
    verify.add_argument("--expected-workers", required=True)
    verify.add_argument("--shard-count", type=int, required=True)
    verify.add_argument("--selection-root", type=Path, required=True)
    verify.add_argument("--artifact-prefix", required=True)
    verify.set_defaults(handler=_verify_command)
    args = parser.parse_args()
    return args.handler(args)


if __name__ == "__main__":
    raise SystemExit(main())
