# Mindclade shared Go libraries

Copyright 2026 Mindclade. All rights reserved. Confidential and proprietary.

`libs/go` is the canonical reusable foundation for Mindclade's fleet control
plane. It contains fully implemented, tested mechanisms that would otherwise be
repeated across APIs, schedulers, controllers, operators, ingestion
coordinators, event projectors, dispatchers, registries, and administrative
processes.

It deliberately does **not** contain domain services, generated APIs, tenancy
policy, scheduling decisions, ingestion semantics, registry policy, or process
entry points.

## Package layers

| Layer | Packages | Responsibility |
|---|---|---|
| 0 — Foundations | `clock`, `faults`, `identifiers` | Time, structured failures, canonical identities |
| 1 — Contracts | `audit`, `auth`, `idempotency`, `messaging`, `pagination`, `requestmeta`, `resourceversion`, `signing`, narrow `storage/*` contracts | Security, lineage, delivery, replay safety, concurrency, signing, persistence contracts |
| 2 — Mechanisms | `config`, `observability`, `retry`, `servicekit`, `servicekit/production`, `coordination/*` | Configuration, telemetry, lifecycle, fencing, outbox/inbox, cursors, projectors, leased work |
| 3 — Adapters | `kubernetes/*`, `*/postgres`, GCS, Redis, memory/conformance adapters, SQL migrations | Production infrastructure integration |
| 4 — Transports | `httpx`, `httpx/outbound`, `connectx`, `grpcx` | Standard inbound/outbound network behavior and wire-fault translation |

Layer 5 consumers live outside this directory under `control/`, `services/`,
`operators/`, and `workers/`.

## Mandatory production path

Every production Go executable composes through:

```text
service-owned typed configuration and provider construction
    -> services/.../internal/foundation.Dependencies
    -> servicekit/production.Builder
    -> role capability validation
    -> staged servicekit lifecycle
    -> bounded signals, health, drain, cancellation, and reverse shutdown
```

Direct `servicekit.New`, ad hoc signal handling, detached coordination loops,
service-local outbox implementations, and process-specific health frameworks
are prohibited outside low-level tests and qualification harnesses.

## Durable coordination

The foundation standardizes the correctness-sensitive mechanisms most likely to
be duplicated incorrectly:

- transactional outbox storage and fenced dispatch;
- transactional inbox/idempotency processing;
- monotonic compare-and-swap cursors;
- leased work queues with fencing, heartbeat, retry, and dead-letter states;
- singleton leadership through the canonical lease contract;
- projector loops with atomic effects, deduplication, and cursor advancement;
- forward-only checksummed PostgreSQL migrations;
- process-wide configuration snapshots, provenance, redaction, and safe reload.

## Dependency rules

- Import the narrowest package that owns the mechanism.
- Lower layers never import transports or provider-specific adapters.
- Provider clients stay in composition roots when a stable foundation contract
  exists.
- No nested `go.mod` or `go.sum` files are allowed under `libs/go`.
- No catch-all `common`, `helpers`, `shared`, or `utils` package is allowed.
- Production code must not import conformance or test-only packages.
- Cross-language wire primitives are defined under `protocols/` and qualified
  through Go/Rust/Python golden vectors.

See [`LAYERS.md`](LAYERS.md) for dependency law and
[`CONSUMPTION.md`](CONSUMPTION.md) for the process-by-process adoption matrix.
Adoption the matrix describes is the target; what processes actually link is
generated from the import graph into
`services/control_plane/internal/bootstrap/consumption.json`, and packages that
nothing imports are recorded with their reason in
[`UNCONSUMED.toml`](UNCONSUMED.toml).

See [`USAGE.md`](USAGE.md) for package selection, code patterns, provider composition, durable coordination recipes, and testing guidance.

## Qualification

```bash
gofmt -w ./libs/go
go vet ./libs/go/...
go test ./libs/go/...
go test -race ./libs/go/...
tools/dev/bazelw test //libs/go/... --config=ci
```

Connected CI additionally runs PostgreSQL, Redis, GCS, Kubernetes, Connect,
gRPC, and OpenTelemetry integration suites against the monorepo-pinned real
dependencies and verifies the Bazel/Nix toolchain contract.
