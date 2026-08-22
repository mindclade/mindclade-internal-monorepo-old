# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Qualify the rolling affected-presubmit latency objective from retained evidence."""

from __future__ import annotations

import argparse
import json
import math
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
WINDOW_DAYS = 28
MINIMUM_SAMPLES = 20
SLO_SECONDS = 30 * 60


@dataclass(frozen=True)
class Metric:
    completed_at: datetime
    elapsed_seconds: float


def _timestamp(value: Any) -> datetime:
    if not isinstance(value, str):
        raise ValueError("completed_at must be an RFC3339 string")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("completed_at must include a timezone")
    return parsed.astimezone(UTC)


def load_metric(path: Path) -> Metric | None:
    if path.is_symlink() or path.stat().st_size > 64 * 1024:
        raise ValueError(f"unsafe metric artifact: {path}")
    payload = json.loads(path.read_text(encoding="utf-8"))
    if type(payload.get("schema_version")) is not int or payload["schema_version"] != 1:
        raise ValueError(f"unsupported metric schema: {path}")
    if payload.get("event") != "pull_request" or payload.get("mode") != "affected":
        return None
    elapsed = payload.get("job_elapsed_seconds")
    if isinstance(elapsed, bool) or not isinstance(elapsed, (int, float)) or elapsed < 0:
        raise ValueError(f"invalid affected latency in {path}")
    return Metric(completed_at=_timestamp(payload.get("completed_at")), elapsed_seconds=elapsed)


def qualify(metrics: list[Metric], *, now: datetime) -> tuple[dict[str, Any], int]:
    now = now.astimezone(UTC)
    start = now - timedelta(days=WINDOW_DAYS)
    window = sorted(
        (metric for metric in metrics if start <= metric.completed_at <= now),
        key=lambda metric: metric.elapsed_seconds,
    )
    first_sample = min((metric.completed_at for metric in metrics), default=None)
    burn_in_complete = first_sample is not None and now - first_sample >= timedelta(
        days=WINDOW_DAYS
    )
    p95 = None
    if window:
        rank = max(1, math.ceil(0.95 * len(window)))
        p95 = round(window[rank - 1].elapsed_seconds, 3)

    if not burn_in_complete:
        status = "burn_in"
        exit_code = 0
    elif len(window) < MINIMUM_SAMPLES:
        status = "insufficient_evidence"
        exit_code = 1
    elif p95 is not None and p95 <= SLO_SECONDS:
        status = "passed"
        exit_code = 0
    else:
        status = "failed"
        exit_code = 1
    return (
        {
            "schema_version": SCHEMA_VERSION,
            "status": status,
            "window_days": WINDOW_DAYS,
            "window_started_at": start.isoformat().replace("+00:00", "Z"),
            "evaluated_at": now.isoformat().replace("+00:00", "Z"),
            "first_sample_at": (
                first_sample.isoformat().replace("+00:00", "Z") if first_sample else None
            ),
            "burn_in_complete": burn_in_complete,
            "sample_count": len(window),
            "minimum_samples": MINIMUM_SAMPLES,
            "p95_seconds": p95,
            "objective_seconds": SLO_SECONDS,
        },
        exit_code,
    )


def _markdown(payload: dict[str, Any]) -> str:
    return "\n".join(
        [
            "## Affected Bazel latency SLO",
            "",
            f"- Status: `{payload['status']}`",
            f"- 28-day samples: `{payload['sample_count']}` "
            f"(minimum `{payload['minimum_samples']}`)",
            f"- p95: `{payload['p95_seconds']}` seconds",
            f"- Objective: `{payload['objective_seconds']}` seconds",
            "",
        ]
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--metrics-root", type=Path, required=True)
    parser.add_argument("--json-output", type=Path, required=True)
    parser.add_argument("--markdown-output", type=Path, required=True)
    parser.add_argument("--now")
    args = parser.parse_args()
    now = _timestamp(args.now) if args.now else datetime.now(UTC)
    metrics = [
        metric
        for path in sorted(args.metrics_root.rglob("*.json"))
        if (metric := load_metric(path)) is not None
    ]
    payload, exit_code = qualify(metrics, now=now)
    args.json_output.write_text(
        json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    args.markdown_output.write_text(_markdown(payload), encoding="utf-8")
    print(_markdown(payload), end="")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
