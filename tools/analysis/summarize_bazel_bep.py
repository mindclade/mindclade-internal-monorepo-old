#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Normalize Bazel JSON Build Event Protocol metrics into JSON and Markdown evidence."""

from __future__ import annotations

import argparse
import gzip
import json
from collections import Counter
from pathlib import Path
from typing import TextIO


class BepError(ValueError):
    """The BEP file is malformed or lacks required metrics."""


def _integer(value: object) -> int:
    if isinstance(value, bool):
        return 0
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return 0
    return 0


def _duration_ms(value: object) -> int:
    if isinstance(value, str) and value.endswith("s"):
        try:
            return round(float(value[:-1]) * 1000)
        except ValueError:
            return 0
    return _integer(value)


def _open(path: Path) -> TextIO:
    if path.suffix == ".gz":
        return gzip.open(path, "rt", encoding="utf-8")
    return path.open(encoding="utf-8")


def summarize(path: Path, label: str) -> dict[str, object]:
    build_metrics: dict[str, object] | None = None
    command = "unknown"
    test_statuses: Counter[str] = Counter()
    test_attempts = 0
    test_duration_ms = 0

    try:
        with _open(path) as handle:
            for line_number, raw_line in enumerate(handle, start=1):
                if not raw_line.strip():
                    continue
                try:
                    event = json.loads(raw_line)
                except json.JSONDecodeError as error:
                    raise BepError(f"{path}:{line_number}: invalid JSON: {error}") from error
                if not isinstance(event, dict):
                    raise BepError(f"{path}:{line_number}: event must be a JSON object")
                started = event.get("started")
                if isinstance(started, dict) and isinstance(started.get("command"), str):
                    command = started["command"]
                metrics = event.get("buildMetrics")
                if isinstance(metrics, dict):
                    build_metrics = metrics
                test_summary = event.get("testSummary")
                if isinstance(test_summary, dict):
                    status = test_summary.get("overallStatus", "UNKNOWN")
                    test_statuses[str(status)] += 1
                    attempts = test_summary.get("attemptCount", 0)
                    duration = test_summary.get("totalRunDurationMillis", 0)
                    test_attempts += _integer(attempts)
                    test_duration_ms += _integer(duration)
    except OSError as error:
        raise BepError(f"cannot read {path}: {error}") from error

    if build_metrics is None:
        raise BepError(f"{path}: no buildMetrics event found")

    action = build_metrics.get("actionSummary", {})
    target = build_metrics.get("targetMetrics", {})
    package = build_metrics.get("packageMetrics", {})
    timing = build_metrics.get("timingMetrics", {})
    if not all(isinstance(value, dict) for value in (action, target, package, timing)):
        raise BepError(f"{path}: malformed buildMetrics sections")
    cache = action.get("actionCacheStatistics", {})
    if not isinstance(cache, dict):
        cache = {}
    runners = action.get("runnerCount", [])
    runner_counts: dict[str, int] = {}
    if isinstance(runners, list):
        for runner in runners:
            if not isinstance(runner, dict):
                continue
            name = runner.get("name")
            count = runner.get("count")
            if isinstance(name, str):
                runner_counts[name] = runner_counts.get(name, 0) + _integer(count)

    critical_path = timing.get("criticalPathTimeInMs")
    if critical_path is None:
        critical_path = _duration_ms(timing.get("criticalPathTime", 0))
    else:
        critical_path = _integer(critical_path)

    return {
        "schema": 1,
        "label": label,
        "command": command,
        "source": path.name,
        "timing_ms": {
            "wall": _integer(timing.get("wallTimeInMs", 0)),
            "cpu": _integer(timing.get("cpuTimeInMs", 0)),
            "analysis": _integer(timing.get("analysisPhaseTimeInMs", 0)),
            "execution": _integer(timing.get("executionPhaseTimeInMs", 0)),
            "critical_path": critical_path,
        },
        "graph": {
            "packages_loaded": _integer(package.get("packagesLoaded", 0)),
            "targets_configured": _integer(target.get("targetsConfigured", 0)),
        },
        "actions": {
            "created": _integer(action.get("actionsCreated", 0)),
            "executed": _integer(action.get("actionsExecuted", 0)),
            "cache_hits": _integer(cache.get("hits", 0)),
            "cache_misses": _integer(cache.get("misses", 0)),
            "runners": dict(sorted(runner_counts.items())),
        },
        "tests": {
            "outcomes": dict(sorted(test_statuses.items())),
            "attempts": test_attempts,
            "total_run_duration_ms": test_duration_ms,
        },
    }


def markdown(summary: dict[str, object]) -> str:
    timing = summary["timing_ms"]
    graph = summary["graph"]
    actions = summary["actions"]
    tests = summary["tests"]
    assert isinstance(timing, dict)
    assert isinstance(graph, dict)
    assert isinstance(actions, dict)
    assert isinstance(tests, dict)
    runners = actions["runners"]
    outcomes = tests["outcomes"]
    assert isinstance(runners, dict)
    assert isinstance(outcomes, dict)
    runner_text = ", ".join(f"{name}={count}" for name, count in runners.items()) or "none"
    outcome_text = ", ".join(f"{name}={count}" for name, count in outcomes.items()) or "none"
    return (
        f"### Bazel {summary['label']} performance\n\n"
        "| Metric | Value |\n"
        "| --- | ---: |\n"
        f"| Wall time (ms) | {timing['wall']} |\n"
        f"| Analysis time (ms) | {timing['analysis']} |\n"
        f"| Execution time (ms) | {timing['execution']} |\n"
        f"| Critical path (ms) | {timing['critical_path']} |\n"
        f"| Packages loaded | {graph['packages_loaded']} |\n"
        f"| Targets configured | {graph['targets_configured']} |\n"
        f"| Actions created / executed | {actions['created']} / {actions['executed']} |\n"
        f"| Action cache hits / misses | {actions['cache_hits']} / {actions['cache_misses']} |\n"
        f"| Runners | {runner_text} |\n"
        f"| Test outcomes | {outcome_text} |\n"
        f"| Test attempts | {tests['attempts']} |\n\n"
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bep", type=Path, required=True)
    parser.add_argument("--label", required=True)
    parser.add_argument("--json-output", type=Path, required=True)
    parser.add_argument("--markdown-output", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        summary = summarize(args.bep, args.label)
    except BepError as error:
        parser.error(str(error))
    args.json_output.write_text(
        json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    args.markdown_output.write_text(markdown(summary), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
