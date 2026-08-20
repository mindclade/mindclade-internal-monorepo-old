# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Contract tests for the executable runtime-gateway benchmark harness."""

from __future__ import annotations

import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from tools.qualification.rust.runtime_gateway_benchmark import measure_gateway, percentile


class _Resolver(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    requests = 0

    def do_POST(self) -> None:
        length = int(self.headers["Content-Length"])
        self.rfile.read(length)
        type(self).requests += 1
        body = b"resolved"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:
        del format, args


def test_percentile_uses_nearest_rank() -> None:
    assert percentile([5.0, 1.0, 4.0, 2.0, 3.0], 0.50) == 3.0
    assert percentile([5.0, 1.0, 4.0, 2.0, 3.0], 0.99) == 5.0
    with pytest.raises(ValueError):
        percentile([], 0.50)


def test_gateway_harness_measures_reuse_and_connection_churn() -> None:
    _Resolver.requests = 0
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Resolver)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        result = measure_gateway(
            f"http://127.0.0.1:{server.server_port}",
            b"protobuf fixture",
            samples=8,
            warmup=2,
        )
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=2)

    assert _Resolver.requests == 18
    assert result["runtime_gateway_latency_p50_ms"] > 0
    assert result["runtime_gateway_latency_p50_ms"] <= result["runtime_gateway_latency_p99_ms"]
    assert result["runtime_gateway_connection_churn_p50_ms"] > 0
    assert result["runtime_gateway_request_overhead_us"] == pytest.approx(
        result["runtime_gateway_latency_p50_ms"] * 1_000.0
    )
