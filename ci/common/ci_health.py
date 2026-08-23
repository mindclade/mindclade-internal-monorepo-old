#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Aggregate bounded Bazel worker evidence into a deterministic CI health dashboard."""

from __future__ import annotations

import argparse
import html
import json
import math
import os
import re
import stat
import sys
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
MAX_JSON_BYTES = 4 * 1024 * 1024
SHA1_PATTERN = re.compile(r"[0-9a-f]{40}")
ARTIFACT_PREFIX_PATTERN = re.compile(
    r"bazel-(?:health|nightly-health|performance|nightly)-[1-9][0-9]*-[1-9][0-9]*-"
)
TOPOLOGY_WORKERS = {
    "presubmit-auto": lambda count: (-1,),
    "full-unsharded": lambda count: (-2,),
    "full-sharded": lambda count: tuple(range(count)),
}


class HealthContractError(RuntimeError):
    """CI health evidence is absent, ambiguous, or malformed."""


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate key")
        value[key] = item
    return value


def _read_json(path: Path) -> dict[str, Any]:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise HealthContractError("CI health evidence is unreadable") from error
    try:
        metadata = os.fstat(descriptor)
        if (
            not stat.S_ISREG(metadata.st_mode)
            or metadata.st_size <= 0
            or metadata.st_size > MAX_JSON_BYTES
            or metadata.st_mode & (stat.S_IWGRP | stat.S_IWOTH)
        ):
            raise HealthContractError("CI health evidence is unsafe")
        chunks = []
        remaining = metadata.st_size + 1
        while remaining:
            chunk = os.read(descriptor, min(64 * 1024, remaining))
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        payload = b"".join(chunks)
        after = os.fstat(descriptor)
        if len(payload) != metadata.st_size or (
            metadata.st_dev,
            metadata.st_ino,
            metadata.st_size,
            metadata.st_mtime_ns,
        ) != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns):
            raise HealthContractError("CI health evidence changed while read")
    except OSError as error:
        raise HealthContractError("CI health evidence is unreadable") from error
    finally:
        os.close(descriptor)
    try:
        value = json.loads(
            payload.decode("utf-8"),
            object_pairs_hook=_unique_object,
            parse_constant=lambda _value: (_ for _ in ()).throw(ValueError("constant")),
        )
    except (UnicodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
        raise HealthContractError("CI health evidence is invalid") from error
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise HealthContractError("CI health evidence is invalid")
    return value


def _number(value: Any, *, integral: bool = False) -> int | float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value < 0:
        raise HealthContractError("CI health metric is invalid")
    if integral and not isinstance(value, int):
        raise HealthContractError("CI health metric is invalid")
    return value


def _integer(value: Any) -> int:
    return int(_number(value, integral=True))


def _optional_number(value: Any) -> int | float | None:
    if value is None:
        return None
    return _number(value)


def _percent(numerator: int, denominator: int) -> float | None:
    return None if denominator == 0 else round(numerator * 100 / denominator, 2)


def _ratio(values: list[int | float]) -> float | None:
    positive = [value for value in values if value > 0]
    return None if len(positive) < 2 else round(max(positive) / min(positive), 3)


def _percentile(values: list[int | float], percentile: float) -> int | float | None:
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(percentile * len(ordered)))
    return ordered[rank - 1]


def _parse_workers(value: str) -> tuple[int, ...]:
    try:
        decoded = json.loads(value)
    except json.JSONDecodeError as error:
        raise HealthContractError("expected worker set is invalid") from error
    if (
        not isinstance(decoded, list)
        or not decoded
        or any(type(worker) is not int for worker in decoded)
        or len(decoded) != len(set(decoded))
    ):
        raise HealthContractError("expected worker set is invalid")
    return tuple(decoded)


def _phase_summary(path: Path, *, required: bool) -> dict[str, Any] | None:
    if path.is_symlink():
        raise HealthContractError("Bazel phase summary is unsafe")
    if not path.exists() and not required:
        return None
    payload = _read_json(path)
    if set(payload) != {
        "actions",
        "command",
        "graph",
        "label",
        "schema",
        "source",
        "tests",
        "timing_ms",
    }:
        raise HealthContractError("Bazel phase summary fields are invalid")
    if payload["schema"] != 2:
        raise HealthContractError("Bazel phase summary schema is unsupported")
    timing = payload["timing_ms"]
    actions = payload["actions"]
    tests = payload["tests"]
    if not isinstance(timing, dict) or not isinstance(actions, dict) or not isinstance(tests, dict):
        raise HealthContractError("Bazel phase summary is invalid")
    for field in ("wall", "cpu", "analysis", "execution", "critical_path"):
        _integer(timing.get(field))
    for field in ("created", "executed", "cache_hits", "cache_misses"):
        _integer(actions.get(field))
    labels = tests.get("non_passing_labels")
    if not isinstance(labels, dict):
        raise HealthContractError("Bazel test outcome labels are invalid")
    for status, values in labels.items():
        if (
            not isinstance(status, str)
            or not isinstance(values, list)
            or any(not isinstance(label, str) or not label.startswith("//") for label in values)
            or values != sorted(set(values))
        ):
            raise HealthContractError("Bazel test outcome labels are invalid")
    return payload


def _worker(
    root: Path,
    *,
    worker: int,
    event: str,
    head_sha: str,
    topology_mode: str,
) -> dict[str, Any]:
    try:
        children = tuple(root.iterdir())
    except OSError as error:
        raise HealthContractError("CI health worker artifact is unreadable") from error
    allowed_files = {"analysis.summary.json", "run-metrics.json", "test.summary.json"}
    if not children or any(child.name not in allowed_files for child in children):
        raise HealthContractError("CI health worker artifact fields are invalid")
    metrics = _read_json(root / "run-metrics.json")
    required_fields = {
        "analysis_graph_sha256",
        "analysis_target_count",
        "completed_at",
        "event",
        "head_sha",
        "job_elapsed_seconds",
        "latency_slo_met",
        "latency_slo_seconds",
        "mode",
        "reason",
        "schema_version",
        "shard_count",
        "shard_index",
        "test_graph_sha256",
        "test_target_count",
    }
    if set(metrics) != required_fields or metrics.get("schema_version") != 1:
        raise HealthContractError("Bazel run metrics fields are invalid")
    if metrics.get("event") != event or metrics.get("head_sha") != head_sha:
        raise HealthContractError("Bazel run metrics identity disagrees")
    mode = metrics.get("mode")
    if mode not in {"affected", "full"}:
        raise HealthContractError("Bazel selection mode is invalid")
    if topology_mode != "presubmit-auto" and mode != "full":
        raise HealthContractError("protected full-graph worker did not run full mode")
    if topology_mode == "presubmit-auto" and event != "pull_request":
        raise HealthContractError("pull-request worker topology is invalid")
    analysis_targets = _integer(metrics.get("analysis_target_count"))
    test_targets = _integer(metrics.get("test_target_count"))
    elapsed = _optional_number(metrics.get("job_elapsed_seconds"))
    analysis = _phase_summary(root / "analysis.summary.json", required=analysis_targets > 0)
    test = _phase_summary(root / "test.summary.json", required=test_targets > 0)

    shard_index = metrics.get("shard_index")
    if topology_mode == "full-sharded":
        if shard_index != worker:
            raise HealthContractError("Bazel shard metrics identity disagrees")
    elif shard_index is not None:
        raise HealthContractError("unsharded Bazel metrics claim a shard index")

    phases: dict[str, Any] = {}
    hits = 0
    misses = 0
    flaky: set[str] = set()
    non_passing: set[str] = set()
    for name, summary in (("analysis", analysis), ("test", test)):
        if summary is None:
            phases[name] = {"available": False}
            continue
        timing = summary["timing_ms"]
        actions = summary["actions"]
        phase_hits = _integer(actions["cache_hits"])
        phase_misses = _integer(actions["cache_misses"])
        hits += phase_hits
        misses += phase_misses
        labels = summary["tests"]["non_passing_labels"]
        for status, values in labels.items():
            non_passing.update(values)
            if status == "FLAKY":
                flaky.update(values)
        phases[name] = {
            "available": True,
            "wall_ms": _integer(timing["wall"]),
            "critical_path_ms": _integer(timing["critical_path"]),
            "critical_path_wall_percent": _percent(
                _integer(timing["critical_path"]), _integer(timing["wall"])
            ),
            "cache_hits": phase_hits,
            "cache_misses": phase_misses,
            "cache_hit_percent": _percent(phase_hits, phase_hits + phase_misses),
        }
    requests = hits + misses
    return {
        "worker": worker,
        "selection_mode": mode,
        "shard_index": shard_index,
        "analysis_target_count": analysis_targets,
        "test_target_count": test_targets,
        "job_elapsed_seconds": elapsed,
        "queue_seconds": None,
        "phases": phases,
        "action_cache": {
            "hits": hits,
            "misses": misses,
            "requests": requests,
            "hit_percent": _percent(hits, requests),
        },
        "flaky_targets": sorted(flaky),
        "non_passing_targets": sorted(non_passing),
    }


def build_dashboard(
    evidence_root: Path,
    *,
    artifact_prefix: str,
    expected_workers: tuple[int, ...],
    lane: str,
    event: str,
    head_sha: str,
    topology_mode: str,
    shard_count: int,
) -> dict[str, Any]:
    if lane not in {"presubmit", "nightly"} or SHA1_PATTERN.fullmatch(head_sha) is None:
        raise HealthContractError("CI health identity is invalid")
    if ARTIFACT_PREFIX_PATTERN.fullmatch(artifact_prefix) is None:
        raise HealthContractError("CI health artifact prefix is invalid")
    if topology_mode not in TOPOLOGY_WORKERS or (
        topology_mode == "full-sharded" and shard_count < 2
    ):
        raise HealthContractError("CI health topology is invalid")
    if expected_workers != TOPOLOGY_WORKERS[topology_mode](shard_count):
        raise HealthContractError("CI health worker topology disagrees")
    if evidence_root.is_symlink() or not evidence_root.is_dir():
        raise HealthContractError("CI health artifact root is invalid")
    expected = {f"{artifact_prefix}{worker}": worker for worker in expected_workers}
    entries = tuple(evidence_root.iterdir())
    if {entry.name for entry in entries} != set(expected):
        raise HealthContractError("CI health artifact set is incomplete")
    workers = []
    for entry in sorted(entries, key=lambda item: expected[item.name]):
        if entry.is_symlink() or not entry.is_dir():
            raise HealthContractError("CI health worker artifact is invalid")
        workers.append(
            _worker(
                entry,
                worker=expected[entry.name],
                event=event,
                head_sha=head_sha,
                topology_mode=topology_mode,
            )
        )

    elapsed = [value for worker in workers if (value := worker["job_elapsed_seconds"]) is not None]
    test_walls = [
        worker["phases"]["test"]["wall_ms"]
        for worker in workers
        if worker["phases"]["test"]["available"]
    ]
    critical_paths = [
        worker["phases"]["test"]["critical_path_ms"]
        for worker in workers
        if worker["phases"]["test"]["available"]
    ]
    hits = sum(worker["action_cache"]["hits"] for worker in workers)
    misses = sum(worker["action_cache"]["misses"] for worker in workers)
    requests = hits + misses
    flaky_targets = sorted({label for worker in workers for label in worker["flaky_targets"]})
    non_passing_targets = sorted(
        {label for worker in workers for label in worker["non_passing_targets"]}
    )
    return {
        "schema_version": SCHEMA_VERSION,
        "identity": {
            "lane": lane,
            "event": event,
            "head_sha": head_sha,
            "topology_mode": topology_mode,
            "expected_workers": list(expected_workers),
            "shard_count": shard_count,
        },
        "measurement_boundaries": {
            "queue_seconds": {
                "status": "unavailable",
                "value": None,
                "reason": (
                    "GitHub does not expose a worker queue-start timestamp inside the job; "
                    "workflow-run creation would include dependency time and is not runner queue time."
                ),
            },
            "job_elapsed_seconds": (
                "Measured from the worker's first step until Bazel selection evidence completes; "
                "artifact upload and verdict time are excluded."
            ),
            "critical_path_ms": (
                "Bazel BEP timingMetrics duration only; action identities remain in the retained "
                "Bazel profile and are not copied into this redacted dashboard."
            ),
            "action_cache": (
                "Bazel BEP actionCacheStatistics across analysis and test commands; transport "
                "restore hits are recorded separately in cache-metrics.json."
            ),
        },
        "aggregate": {
            "worker_count": len(workers),
            "job_elapsed_seconds": {
                "maximum": max(elapsed, default=None),
                "p50": _percentile(elapsed, 0.50),
                "p95": _percentile(elapsed, 0.95),
            },
            "test_wall_ms": {
                "maximum": max(test_walls, default=None),
                "p50": _percentile(test_walls, 0.50),
                "p95": _percentile(test_walls, 0.95),
            },
            "test_critical_path_ms": {
                "maximum": max(critical_paths, default=None),
                "p50": _percentile(critical_paths, 0.50),
                "p95": _percentile(critical_paths, 0.95),
            },
            "action_cache": {
                "hits": hits,
                "misses": misses,
                "requests": requests,
                "hit_percent": _percent(hits, requests),
            },
            "shard_balance": {
                "job_elapsed_max_to_min_ratio": _ratio(elapsed),
                "test_wall_max_to_min_ratio": _ratio(test_walls),
                "test_critical_path_max_to_min_ratio": _ratio(critical_paths),
            },
            "flaky_targets": flaky_targets,
            "non_passing_targets": non_passing_targets,
        },
        "workers": workers,
    }


def _display(value: Any) -> str:
    if value is None:
        return "unavailable"
    if isinstance(value, float):
        return f"{value:.3f}".rstrip("0").rstrip(".")
    return str(value)


def render_html(payload: dict[str, Any]) -> str:
    identity = payload["identity"]
    aggregate = payload["aggregate"]
    rows = []
    for worker in payload["workers"]:
        test = worker["phases"]["test"]
        rows.append(
            "<tr>"
            f"<td>{worker['worker']}</td>"
            f"<td>{html.escape(worker['selection_mode'])}</td>"
            f"<td>{_display(worker['job_elapsed_seconds'])}</td>"
            f"<td>{_display(test.get('wall_ms'))}</td>"
            f"<td>{_display(test.get('critical_path_ms'))}</td>"
            f"<td>{_display(worker['action_cache']['hit_percent'])}</td>"
            f"<td>{worker['test_target_count']}</td>"
            "</tr>"
        )
    flaky = aggregate["flaky_targets"]
    flaky_html = (
        "<ul>" + "".join(f"<li><code>{html.escape(label)}</code></li>" for label in flaky) + "</ul>"
        if flaky
        else "<p>None observed.</p>"
    )
    return f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<title>Mindclade CI health</title>
<style>body{{font:15px system-ui;max-width:1100px;margin:2rem auto;padding:0 1rem;color:#201c24}}table{{border-collapse:collapse;width:100%}}th,td{{border:1px solid #d8d2cb;padding:.55rem;text-align:right}}th:first-child,td:first-child{{text-align:left}}code{{overflow-wrap:anywhere}}.cards{{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:1rem}}.card{{border:1px solid #d8d2cb;border-radius:8px;padding:1rem}}dt{{font-weight:700}}dd{{margin:0}}</style>
</head><body><main><h1>CI health</h1>
<p><code>{html.escape(identity["head_sha"])}</code> · {html.escape(identity["lane"])} · {html.escape(identity["event"])} · {html.escape(identity["topology_mode"])}</p>
<section class="cards" aria-label="Aggregate metrics">
<div class="card"><dl><dt>Action cache hit rate</dt><dd>{_display(aggregate["action_cache"]["hit_percent"])}%</dd></dl></div>
<div class="card"><dl><dt>Worker elapsed p95</dt><dd>{_display(aggregate["job_elapsed_seconds"]["p95"])} s</dd></dl></div>
<div class="card"><dl><dt>Test critical path p95</dt><dd>{_display(aggregate["test_critical_path_ms"]["p95"])} ms</dd></dl></div>
<div class="card"><dl><dt>Shard elapsed ratio</dt><dd>{_display(aggregate["shard_balance"]["job_elapsed_max_to_min_ratio"])}</dd></dl></div>
</section><h2>Workers</h2><table><thead><tr><th>Worker</th><th>Mode</th><th>Elapsed (s)</th><th>Test wall (ms)</th><th>Critical path (ms)</th><th>Cache hit (%)</th><th>Tests selected</th></tr></thead><tbody>{"".join(rows)}</tbody></table>
<h2>Flaky targets</h2>{flaky_html}
<h2>Measurement boundaries</h2><p>{html.escape(payload["measurement_boundaries"]["queue_seconds"]["reason"])}</p>
</main></body></html>
"""


def render_markdown(payload: dict[str, Any]) -> str:
    aggregate = payload["aggregate"]
    return (
        "### Bazel CI health\n\n"
        "| Metric | Value |\n| --- | ---: |\n"
        f"| Action cache hit rate | `{_display(aggregate['action_cache']['hit_percent'])}%` |\n"
        f"| Worker elapsed p95 | `{_display(aggregate['job_elapsed_seconds']['p95'])} s` |\n"
        f"| Test critical path p95 | `{_display(aggregate['test_critical_path_ms']['p95'])} ms` |\n"
        f"| Shard elapsed max/min | `{_display(aggregate['shard_balance']['job_elapsed_max_to_min_ratio'])}` |\n"
        f"| Flaky targets | `{len(aggregate['flaky_targets'])}` |\n\n"
        "Queue time is unavailable: workflow creation includes dependency time and is not runner queue time.\n\n"
    )


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(content, encoding="utf-8")
    os.replace(temporary, path)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence-root", type=Path, required=True)
    parser.add_argument("--artifact-prefix", required=True)
    parser.add_argument("--expected-workers", required=True)
    parser.add_argument("--lane", choices=("presubmit", "nightly"), required=True)
    parser.add_argument("--event", required=True)
    parser.add_argument("--head-sha", required=True)
    parser.add_argument("--topology-mode", choices=tuple(TOPOLOGY_WORKERS), required=True)
    parser.add_argument("--shard-count", type=int, required=True)
    parser.add_argument("--json-output", type=Path, required=True)
    parser.add_argument("--html-output", type=Path, required=True)
    parser.add_argument("--summary-output", type=Path)
    args = parser.parse_args()
    try:
        payload = build_dashboard(
            args.evidence_root,
            artifact_prefix=args.artifact_prefix,
            expected_workers=_parse_workers(args.expected_workers),
            lane=args.lane,
            event=args.event,
            head_sha=args.head_sha,
            topology_mode=args.topology_mode,
            shard_count=args.shard_count,
        )
    except HealthContractError as error:
        print(f"CI_HEALTH_EVIDENCE_INVALID: {error}", file=sys.stderr)
        return 2
    _write(args.json_output, json.dumps(payload, indent=2, sort_keys=True) + "\n")
    _write(args.html_output, render_html(payload))
    if args.summary_output is not None:
        with args.summary_output.open("a", encoding="utf-8") as stream:
            stream.write(render_markdown(payload))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
