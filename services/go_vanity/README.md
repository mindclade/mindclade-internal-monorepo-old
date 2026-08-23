# Go vanity service

`go_vanity` serves the `go-import` response for `go.mindclade.dev`. The canonical repository is
`https://github.com/mindclade/mindclade-internal-monorepo`; the process default and checked-in
development configuration agree on that exact URL.

The process owns two HTTP listeners:

- `LISTEN_ADDRESS` (default `:8080`) serves `/healthz`, `/readyz`, and vanity responses.
- `METRICS_LISTEN_ADDRESS` (default `127.0.0.1:9090`) serves only `/metrics`.

Metrics use a fixed schema: four HTTP status-class counter series, nine cumulative latency buckets,
one latency sum/count pair, readiness, and build identity. Request path, host, repository, module,
tenant, and user values are never labels. Both listeners share one lifecycle: either listener
failing drains the other, and SIGINT/SIGTERM marks readiness false before a bounded shutdown.

The service needs no Kubernetes API token, cloud identity, credential, writable volume, database,
or network egress. This source change does not activate or expose the metrics listener: its default
is loopback-only, the OCI metadata and checked-in Service expose only port 8080, and the development
deployment remains disabled. A deployment change must separately configure a Pod-reachable metrics
address, add a private metrics path, and retain connected qualification evidence.

Run focused tests with:

```sh
tools/dev/nixw develop .#ci --command bazel test //services/go_vanity/...
```
