# Go foundation qualification

This directory owns the reproducible qualification entry point for the shared
Go foundation and its representative integrations.

## Offline lane

The offline lane uses only the Go standard-library packages and adapters whose
dependencies are already present in the scaffold. It verifies formatting,
layering, prohibited paved-road bypasses, placeholder absence, race-enabled
coordination and lifecycle tests, runnable examples, and role capability
manifests.

```bash
tools/qualification/go/validate.sh offline
```

## Connected lane

The connected lane additionally downloads and verifies the full pinned external
Go module graph, verifies `go.sum`, requires `go mod tidy -diff` to be clean, runs
`go vet`, executes the complete race-enabled Go package graph, and invokes Bazel
tests. The committed root `go.sum` includes authenticated checksums for every
direct public requirement; the connected lane is authoritative for full
transitive closure.

```bash
tools/qualification/go/validate.sh connected
```

Provider-specific suites must run against ephemeral PostgreSQL, Redis, GCS,
Pub/Sub-compatible delivery, Kubernetes, Connect, gRPC, and OpenTelemetry
environments. Those systems are not silently replaced by memory adapters in a
production qualification lane.
