# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Durable, bounded JobSet terminal-outcome aggregation.

The ledger consumes Kubernetes JobSet objects but never emits a JobSet name or UID label. A
connected watcher remains environment-owned; this module owns idempotence, conflict handling,
bounded state, durable checkpoint format, and OpenMetrics exposition.
"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import re
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Final, cast

SCHEMA_VERSION: Final = 1
DEFAULT_MAXIMUM_TERMINAL_UIDS: Final = 1_000_000
MAXIMUM_EVENT_CHARACTERS: Final = 4 << 20
_LABEL = re.compile(r"[a-z0-9][a-z0-9_.-]{0,62}")
_RESOURCE_VERSION = re.compile(r"[0-9]+")
_FAILURE_REASONS = {
    "DeadlineExceeded": "deadline",
    "FailedJobs": "workload",
    "JobFailurePolicy": "policy",
    "StartupPolicy": "startup",
    "Suspended": "suspended",
}


def _as_dict(value: object) -> dict[str, Any]:
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


def _as_list(value: object) -> list[Any]:
    return cast("list[Any]", value) if isinstance(value, list) else []


@dataclass(frozen=True, order=True)
class TerminalRecord:
    namespace: str
    result: str
    reason_class: str


@dataclass
class OutcomeLedger:
    cluster: str
    maximum_terminal_uids: int = DEFAULT_MAXIMUM_TERMINAL_UIDS
    _seen: dict[str, TerminalRecord] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if not _LABEL.fullmatch(self.cluster):
            raise ValueError("cluster must be a bounded metric label")
        if (
            isinstance(self.maximum_terminal_uids, bool)
            or not isinstance(self.maximum_terminal_uids, int)
            or not 1 <= self.maximum_terminal_uids <= DEFAULT_MAXIMUM_TERMINAL_UIDS
        ):
            raise ValueError("maximum_terminal_uids is outside its fail-closed bound")

    @staticmethod
    def _uid_key(uid: str) -> str:
        return hashlib.sha256(uid.encode("utf-8")).hexdigest()

    @staticmethod
    def _terminal_record(value: dict[str, Any]) -> tuple[str, TerminalRecord] | None:
        api_version = value.get("apiVersion")
        if (
            value.get("kind") != "JobSet"
            or not isinstance(api_version, str)
            or not api_version.startswith("jobset.x-k8s.io/")
        ):
            raise ValueError("outcome input must be a JobSet object")
        if not isinstance(value.get("metadata"), dict):
            raise ValueError("JobSet metadata must be an object")
        metadata = _as_dict(value.get("metadata"))
        uid = metadata.get("uid")
        namespace = metadata.get("namespace")
        resource_version = metadata.get("resourceVersion")
        if not isinstance(uid, str) or not uid or len(uid) > 256:
            raise ValueError("JobSet metadata.uid must be non-empty and bounded")
        if not isinstance(namespace, str) or not _LABEL.fullmatch(namespace):
            raise ValueError("JobSet namespace is not a bounded metric label")
        if not isinstance(resource_version, str) or not _RESOURCE_VERSION.fullmatch(
            resource_version
        ):
            raise ValueError("JobSet resourceVersion must be numeric")
        if "status" in value and not isinstance(value.get("status"), dict):
            raise ValueError("JobSet status must be an object")
        status = _as_dict(value.get("status"))
        if "conditions" in status and not isinstance(status.get("conditions"), list):
            raise ValueError("JobSet status.conditions must be a list")
        conditions = _as_list(status.get("conditions"))
        terminal: list[dict[str, Any]] = []
        for condition in conditions:
            if not isinstance(condition, dict):
                raise ValueError("JobSet conditions must be objects")
            if condition.get("status") not in {"True", "False", "Unknown"}:
                raise ValueError("JobSet condition status is invalid")
            if condition.get("status") != "True":
                continue
            if condition.get("type") in {"Completed", "Failed"}:
                terminal.append(condition)
        if not terminal:
            return None
        if len(terminal) != 1:
            raise ValueError("JobSet may not have conflicting true terminal conditions")
        condition = terminal[0]
        if condition["type"] == "Completed":
            record = TerminalRecord(namespace, "completed", "completed")
        else:
            record = TerminalRecord(
                namespace,
                "failed",
                _FAILURE_REASONS.get(str(condition.get("reason", "")), "other"),
            )
        return OutcomeLedger._uid_key(uid), record

    def observe(self, value: dict[str, Any]) -> bool:
        """Record one terminal outcome exactly once; return whether the counter advanced."""

        parsed = self._terminal_record(value)
        if parsed is None:
            return False
        key, record = parsed
        previous = self._seen.get(key)
        if previous is not None:
            if previous != record:
                raise ValueError("a previously recorded JobSet changed terminal outcome")
            return False
        if len(self._seen) >= self.maximum_terminal_uids:
            raise RuntimeError("terminal-outcome ledger is full; refusing lossy eviction")
        self._seen[key] = record
        return True

    def counts(self) -> dict[TerminalRecord, int]:
        result: dict[TerminalRecord, int] = {}
        for record in self._seen.values():
            result[record] = result.get(record, 0) + 1
        return result

    def openmetrics(self) -> str:
        lines = [
            "# HELP mindclade_jobset_terminal_outcomes_total Durable JobSet terminal transitions.",
            "# TYPE mindclade_jobset_terminal_outcomes_total counter",
        ]
        for record, count in sorted(self.counts().items()):
            lines.append(
                "mindclade_jobset_terminal_outcomes_total"
                f'{{cluster="{self.cluster}",namespace="{record.namespace}",'
                f'result="{record.result}",reason_class="{record.reason_class}"}} {count}'
            )
        lines.extend(
            [
                "# HELP mindclade_jobset_outcome_ledger_records Number of terminal UIDs retained for replay protection.",
                "# TYPE mindclade_jobset_outcome_ledger_records gauge",
                f'mindclade_jobset_outcome_ledger_records{{cluster="{self.cluster}"}} {len(self._seen)}',
                "# HELP mindclade_jobset_outcome_ledger_capacity Maximum terminal UIDs retained before collection fails closed.",
                "# TYPE mindclade_jobset_outcome_ledger_capacity gauge",
                f'mindclade_jobset_outcome_ledger_capacity{{cluster="{self.cluster}"}} {self.maximum_terminal_uids}',
                "# HELP mindclade_jobset_outcome_ledger_utilization_ratio Fraction of replay-protection capacity currently used.",
                "# TYPE mindclade_jobset_outcome_ledger_utilization_ratio gauge",
                f'mindclade_jobset_outcome_ledger_utilization_ratio{{cluster="{self.cluster}"}} {len(self._seen) / self.maximum_terminal_uids:.12g}',
                "# EOF",
            ]
        )
        return "\n".join(lines) + "\n"

    def snapshot(self) -> dict[str, Any]:
        return {
            "schemaVersion": SCHEMA_VERSION,
            "cluster": self.cluster,
            "maximumTerminalUids": self.maximum_terminal_uids,
            "seen": {
                key: {
                    "namespace": record.namespace,
                    "result": record.result,
                    "reasonClass": record.reason_class,
                }
                for key, record in sorted(self._seen.items())
            },
        }

    @classmethod
    def restore(cls, value: dict[str, Any]) -> OutcomeLedger:
        if set(value) != {"schemaVersion", "cluster", "maximumTerminalUids", "seen"}:
            raise ValueError("checkpoint has unsupported or missing fields")
        if value.get("schemaVersion") != SCHEMA_VERSION:
            raise ValueError("unsupported outcome checkpoint schemaVersion")
        cluster = value.get("cluster")
        maximum = value.get("maximumTerminalUids")
        if (
            not isinstance(cluster, str)
            or isinstance(maximum, bool)
            or not isinstance(maximum, int)
        ):
            raise ValueError("checkpoint identity and bound must have exact types")
        ledger = cls(cluster, maximum)
        raw_seen = value.get("seen")
        if not isinstance(raw_seen, dict) or len(raw_seen) > ledger.maximum_terminal_uids:
            raise ValueError("checkpoint seen set is invalid or exceeds its bound")
        for key, raw_record in raw_seen.items():
            if not re.fullmatch(r"[0-9a-f]{64}", str(key)) or not isinstance(raw_record, dict):
                raise ValueError("checkpoint contains a malformed replay key")
            if set(raw_record) != {"namespace", "result", "reasonClass"}:
                raise ValueError("checkpoint terminal record has unsupported fields")
            record = TerminalRecord(
                str(raw_record.get("namespace", "")),
                str(raw_record.get("result", "")),
                str(raw_record.get("reasonClass", "")),
            )
            if (
                not _LABEL.fullmatch(record.namespace)
                or record.result not in {"completed", "failed"}
                or record.reason_class
                not in set(_FAILURE_REASONS.values()) | {"completed", "other"}
            ):
                raise ValueError("checkpoint contains an invalid terminal record")
            ledger._seen[str(key)] = record
        return ledger


def atomic_write(path: Path, payload: str, *, mode: int = 0o600) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            output.write(payload)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    except BaseException:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)
        raise


def load_checkpoint(path: Path, cluster: str, maximum: int) -> OutcomeLedger:
    if not path.exists():
        return OutcomeLedger(cluster, maximum)
    if path.stat().st_mode & 0o077:
        raise ValueError("checkpoint permissions must not allow group or world access")
    if path.stat().st_size > maximum * 256 + 4096:
        raise ValueError("checkpoint exceeds its configured fail-closed size bound")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("checkpoint must be one JSON object")
    ledger = OutcomeLedger.restore(value)
    if ledger.cluster != cluster or ledger.maximum_terminal_uids != maximum:
        raise ValueError("checkpoint identity or configured bound changed")
    return ledger


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--cluster", required=True)
    parser.add_argument("--checkpoint", type=Path, required=True)
    parser.add_argument("--events", type=Path, help="JSON-lines watch events; stdin when omitted")
    parser.add_argument("--metrics-output", type=Path)
    parser.add_argument("--maximum-terminal-uids", type=int, default=DEFAULT_MAXIMUM_TERMINAL_UIDS)
    args = parser.parse_args()
    try:
        ledger = load_checkpoint(args.checkpoint, args.cluster, args.maximum_terminal_uids)
        stream = args.events.open(encoding="utf-8") if args.events else sys.stdin
        try:
            line_number = 0
            while True:
                line = stream.readline(MAXIMUM_EVENT_CHARACTERS + 1)
                if not line:
                    break
                line_number += 1
                if len(line) > MAXIMUM_EVENT_CHARACTERS:
                    raise ValueError(f"event line {line_number} exceeds its size bound")
                if not line.strip():
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError(f"event line {line_number} is not an object")
                value = event.get("object", event)
                if not isinstance(value, dict):
                    raise ValueError(f"event line {line_number} has no object")
                if ledger.observe(value):
                    atomic_write(
                        args.checkpoint,
                        json.dumps(ledger.snapshot(), sort_keys=True, separators=(",", ":")) + "\n",
                    )
        finally:
            if args.events:
                stream.close()
        metrics = ledger.openmetrics()
        if args.metrics_output:
            atomic_write(args.metrics_output, metrics, mode=0o644)
        else:
            sys.stdout.write(metrics)
        return 0
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
