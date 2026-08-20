# Performance qualification

Performance tests are executable harness contracts; promotion numbers are
generated on the declared hardware rather than asserted from shared CI hosts.

The runtime-gateway harness sends a canonical serialized
`RuntimeDispatchRequest` to `POST /v1/runtime/resolve`, warms a persistent
connection, and records nearest-rank p50/p95/p99 latency for both connection
reuse and TCP connection churn. `--pid` also records RSS and, when the platform
supports it, open file descriptors.

```bash
python tools/qualification/rust/runtime_gateway_benchmark.py \
  --url http://127.0.0.1:8080 \
  --request /absolute/path/to/signed-request.pb \
  --pid "$GATEWAY_PID" \
  --output gateway-results.json
```

Release qualification runs this harness itself. A hardware/provider benchmark
command must print one JSON object containing the remaining metrics (including
cancellation, IPC, verified ranges, worker startup, checkpoint, copy, and
allocation measurements). The release wrapper merges those fresh results with
the portable Rust probe and rejects missing budgets; it no longer accepts a
path to pre-authored result JSON.
