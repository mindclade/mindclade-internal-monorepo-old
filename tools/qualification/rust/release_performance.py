#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Collect fresh gateway and H100/H200 evidence, then enforce every Rust budget."""

from __future__ import annotations

import json
import os
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[3]
REQUIRED_ENVIRONMENT = (
    "MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_URL",
    "MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_REQUEST",
    "MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_PID",
    "MINDCLADE_RUNTIME_GATEWAY_CANCELLATION_COMMAND",
    "MINDCLADE_GKE_CLUSTER_CONTEXT",
    "MINDCLADE_QUALIFICATION_IMAGE",
    "MINDCLADE_QUALIFICATION_RUN_ID",
)


def parse_cancellation(stdout: str) -> dict[str, float]:
    value = json.loads(stdout)
    if (
        not isinstance(value, dict)
        or set(value) != {"runtime_gateway_cancellation_ms"}
        or isinstance(value["runtime_gateway_cancellation_ms"], bool)
        or not isinstance(value["runtime_gateway_cancellation_ms"], (int, float))
        or value["runtime_gateway_cancellation_ms"] <= 0
    ):
        raise ValueError("gateway cancellation probe must emit one positive millisecond metric")
    return {"runtime_gateway_cancellation_ms": float(value["runtime_gateway_cancellation_ms"])}


def aggregate_gke_evidence(value: Any) -> dict[str, float]:
    if not isinstance(value, dict) or value.get("schema_version") != 1:
        raise ValueError("GKE hardware evidence schema is invalid")
    profiles = value.get("profiles")
    if not isinstance(profiles, dict) or set(profiles) != {"h100", "h200"}:
        raise ValueError("GKE hardware evidence must contain H100 and H200 profiles")
    for name, metrics in profiles.items():
        if not isinstance(metrics, dict) or metrics.get("hardware_profile") != name:
            raise ValueError(f"GKE {name} evidence identity is invalid")

    def numbers(metric: str) -> list[float]:
        values = []
        for name in ("h100", "h200"):
            candidate = profiles[name].get(metric)
            if (
                isinstance(candidate, bool)
                or not isinstance(candidate, (int, float))
                or candidate <= 0
            ):
                raise ValueError(f"GKE evidence metric is invalid: {name}.{metric}")
            values.append(float(candidate))
        return values

    minimum_metrics = (
        "checkpoint_staging_mib_per_s",
        "unix_ipc_mib_per_s",
        "verified_range_4k_ops_per_s",
        "local_store_contended_4k_ops_per_s",
    )
    maximum_metrics = (
        "data_stream_copy_bytes_per_byte",
        "parser_allocated_bytes_per_input_byte",
    )
    result = {metric: min(numbers(metric)) for metric in minimum_metrics}
    result.update({metric: max(numbers(metric)) for metric in maximum_metrics})
    result["node_stage_start_ms"] = max(numbers("worker_startup_p95_ms"))
    for profile_index, profile in enumerate(("h100", "h200")):
        for metric in (
            "gpu_memory_bytes",
            "gpu_matmul_p50_ms",
            "gpu_matmul_p95_ms",
            "gpu_matmul_p99_ms",
            "gpu_peak_allocated_bytes",
        ):
            result[f"{profile}_{metric}"] = numbers(metric)[profile_index]
    return result


def required_environment() -> dict[str, str]:
    values = {name: os.environ.get(name, "") for name in REQUIRED_ENVIRONMENT}
    missing = sorted(name for name, value in values.items() if not value)
    if missing:
        raise ValueError(f"release performance environment is incomplete: {missing}")
    return values


def main() -> int:
    try:
        environment = required_environment()
        cancellation_command = shlex.split(
            environment["MINDCLADE_RUNTIME_GATEWAY_CANCELLATION_COMMAND"]
        )
        if not cancellation_command:
            raise ValueError("gateway cancellation command is empty")
        with tempfile.TemporaryDirectory(prefix="mindclade-performance-") as temporary_value:
            temporary = Path(temporary_value)
            gateway_results = temporary / "gateway.json"
            cancellation_results = temporary / "cancellation.json"
            gke_evidence = temporary / "gke-hardware.json"
            hardware_results = temporary / "hardware.json"
            subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "tools/qualification/rust/runtime_gateway_benchmark.py"),
                    "--url",
                    environment["MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_URL"],
                    "--request",
                    environment["MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_REQUEST"],
                    "--pid",
                    environment["MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_PID"],
                    "--output",
                    str(gateway_results),
                ],
                cwd=ROOT,
                check=True,
            )
            cancellation = subprocess.run(
                cancellation_command,
                cwd=ROOT,
                check=True,
                capture_output=True,
                text=True,
            )
            cancellation_results.write_text(
                json.dumps(parse_cancellation(cancellation.stdout), sort_keys=True, indent=2) + "\n"
            )
            subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "tools/qualification/gke/run.py"),
                    "--context",
                    environment["MINDCLADE_GKE_CLUSTER_CONTEXT"],
                    "--image",
                    environment["MINDCLADE_QUALIFICATION_IMAGE"],
                    "--run-id",
                    environment["MINDCLADE_QUALIFICATION_RUN_ID"],
                    "--output",
                    str(gke_evidence),
                ],
                cwd=ROOT,
                check=True,
            )
            hardware = aggregate_gke_evidence(json.loads(gke_evidence.read_text()))
            hardware_results.write_text(json.dumps(hardware, sort_keys=True, indent=2) + "\n")
            return subprocess.call(
                [
                    sys.executable,
                    str(ROOT / "tools/qualification/rust/performance.py"),
                    "--measure",
                    "--results",
                    str(gateway_results),
                    "--results",
                    str(cancellation_results),
                    "--results",
                    str(hardware_results),
                    "--require-complete",
                ],
                cwd=ROOT,
            )
    except (
        OSError,
        ValueError,
        json.JSONDecodeError,
        subprocess.CalledProcessError,
    ) as error:
        print(f"release performance failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
