#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Repeatable black-box latency and connection-churn probe for runtime-gateway."""

from __future__ import annotations

import argparse
import http.client
import json
import math
import os
import subprocess
import time
from pathlib import Path
from urllib.parse import SplitResult, urlsplit

MAXIMUM_SAMPLES = 100_000
MAXIMUM_REQUEST_BYTES = 1024 * 1024


def percentile(samples: list[float], quantile: float) -> float:
    """Return a nearest-rank percentile for non-empty finite samples."""
    if not samples or not 0.0 < quantile <= 1.0:
        raise ValueError("percentile input is invalid")
    ordered = sorted(samples)
    rank = max(0, math.ceil(quantile * len(ordered)) - 1)
    return ordered[rank]


def _connection(parsed: SplitResult) -> http.client.HTTPConnection:
    if parsed.hostname is None:
        raise ValueError("gateway URL is missing a host")
    if parsed.scheme == "http":
        return http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=10.0)
    if parsed.scheme == "https":
        return http.client.HTTPSConnection(parsed.hostname, parsed.port or 443, timeout=10.0)
    raise ValueError("gateway URL must use http or https")


def _request(
    connection: http.client.HTTPConnection,
    path: str,
    body: bytes,
) -> float:
    started = time.perf_counter_ns()
    connection.request(
        "POST",
        path,
        body=body,
        headers={
            "Content-Type": "application/x-protobuf",
            "Content-Length": str(len(body)),
        },
    )
    response = connection.getresponse()
    response.read()
    elapsed_ms = (time.perf_counter_ns() - started) / 1_000_000.0
    if response.status != 200:
        raise RuntimeError(f"gateway resolver returned HTTP {response.status}")
    return elapsed_ms


def _summary(prefix: str, samples: list[float]) -> dict[str, float]:
    return {
        f"{prefix}_p50_ms": percentile(samples, 0.50),
        f"{prefix}_p95_ms": percentile(samples, 0.95),
        f"{prefix}_p99_ms": percentile(samples, 0.99),
    }


def measure_gateway(
    base_url: str,
    request: bytes,
    *,
    samples: int,
    warmup: int,
) -> dict[str, float]:
    """Measure resolver latency with reuse and with a new TCP connection."""
    if not request or len(request) > MAXIMUM_REQUEST_BYTES:
        raise ValueError("gateway request size is invalid")
    if samples <= 0 or samples > MAXIMUM_SAMPLES or warmup < 0 or warmup > samples:
        raise ValueError("gateway sample count is invalid")
    parsed = urlsplit(base_url)
    if parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError("gateway URL contains unsupported components")
    base_path = parsed.path.rstrip("/")
    path = f"{base_path}/v1/runtime/resolve"

    connection = _connection(parsed)
    try:
        for _ in range(warmup):
            _request(connection, path, request)
        reused = [_request(connection, path, request) for _ in range(samples)]
    finally:
        connection.close()

    churn = []
    for _ in range(samples):
        connection = _connection(parsed)
        try:
            churn.append(_request(connection, path, request))
        finally:
            connection.close()

    result = _summary("runtime_gateway_latency", reused)
    result.update(_summary("runtime_gateway_connection_churn", churn))
    # The policy's historical name is retained for compatibility. It is the
    # black-box warm-connection p50, including transport and route admission.
    result["runtime_gateway_request_overhead_us"] = (
        result["runtime_gateway_latency_p50_ms"] * 1_000.0
    )
    return result


def sample_process(pid: int) -> dict[str, float]:
    """Collect portable best-effort RSS/FD evidence for the benchmarked pod."""
    if pid <= 0:
        raise ValueError("pid must be positive")
    result: dict[str, float] = {}
    proc = Path(f"/proc/{pid}")
    if proc.is_dir():
        status = (proc / "status").read_text()
        for line in status.splitlines():
            if line.startswith("VmRSS:"):
                result["runtime_gateway_rss_bytes"] = float(line.split()[1]) * 1024.0
                break
        result["runtime_gateway_fd_count"] = float(len(list((proc / "fd").iterdir())))
        return result

    ps = subprocess.run(
        ["ps", "-o", "rss=", "-p", str(pid)],
        check=True,
        capture_output=True,
        text=True,
    )
    result["runtime_gateway_rss_bytes"] = float(ps.stdout.strip()) * 1024.0
    lsof = subprocess.run(
        ["lsof", "-n", "-P", "-p", str(pid)],
        check=False,
        capture_output=True,
        text=True,
    )
    if lsof.returncode == 0:
        result["runtime_gateway_fd_count"] = float(max(0, len(lsof.stdout.splitlines()) - 1))
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--request", required=True, type=Path)
    parser.add_argument("--samples", type=int, default=1_000)
    parser.add_argument("--warmup", type=int, default=100)
    parser.add_argument("--pid", type=int)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    request = args.request.read_bytes()
    result = measure_gateway(
        args.url,
        request,
        samples=args.samples,
        warmup=args.warmup,
    )
    if args.pid is not None:
        result.update(sample_process(args.pid))
    encoded = json.dumps(result, sort_keys=True, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded)
    else:
        print(encoded, end="")
    return os.EX_OK


if __name__ == "__main__":
    raise SystemExit(main())
