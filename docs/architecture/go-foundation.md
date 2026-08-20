# Go foundation architecture

## Boundary

`libs/go` owns reusable control-plane mechanisms that multiple Go processes
would otherwise duplicate incorrectly. It does not own tenancy, quota,
scheduling, registry, ingestion, run, model, dataset, or scientific policy.

```text
libs/go       mechanisms and production adapters
control/      durable domain policy and repositories
services/     composition roots and process lifecycle
protocols/    canonical wire contracts
```

## Layers

| Layer | Packages | Purpose |
|---|---|---|
| 0 | `clock`, `faults`, `identifiers` | Standard-library foundations |
| 1 | auth/audit/idempotency/request metadata, messaging contracts, pagination, resource versions, signing, narrow storage contracts | Stable portable contracts |
| 2 | config, observability, retry, servicekit, servicekit/production, coordination | Runtime and durable mechanisms |
| 3 | PostgreSQL, Redis, GCS, Kubernetes, Pub/Sub, memory/conformance adapters | Infrastructure adapters |
| 4 | HTTP, safe outbound HTTP, Connect, gRPC | Transport adapters |
| 5 | `control/`, `services/`, operators, workers | Consumers outside `libs/go` |

## Durable coordination

The shared foundation standardizes four distinct correctness mechanisms:

```text
outbox       publish durable events after transaction commit
inbox+cursor apply at-least-once events idempotently and in order
workqueue    execute long-running retryable leased work
leadership   establish singleton authority with fencing
```

They are not interchangeable. A projector uses inbox and cursor; a worker uses
a work queue; a request mutation appends to an outbox.

## One service mechanism

All production Go processes use `servicekit/production`:

```text
provider construction
  -> typed foundation.Dependencies
  -> role capability validation
  -> canonical lifecycle stages
  -> readiness, liveness, drain, signals, reverse shutdown
```

The builder distinguishes passive stores from active loops so a process cannot
appear ready merely because it constructed a database table or interface.

## Provider rules

One production adapter is used for each capability: PostgreSQL lifecycle and
transactions, PostgreSQL coordination, Pub/Sub messaging, GCS blob storage,
Redis cache, Kubernetes, and hardened outbound HTTP. Provider configuration
belongs in composition roots. Domain packages depend on narrow contracts.

## Adoption

See:

- [`libs/go/README.md`](https://github.com/mindclade/mindclade-internal-monorepo/blob/HEAD/libs/go/README.md)
- [`libs/go/LAYERS.md`](https://github.com/mindclade/mindclade-internal-monorepo/blob/HEAD/libs/go/LAYERS.md)
- [`libs/go/CONSUMPTION.md`](https://github.com/mindclade/mindclade-internal-monorepo/blob/HEAD/libs/go/CONSUMPTION.md)
- [`../guides/libs-go-module-reference.md`](../guides/libs-go-module-reference.md)
- [`../guides/libs-go-recipes.md`](../guides/libs-go-recipes.md)
- [`../guides/go-service-golden-path.md`](../guides/go-service-golden-path.md)

## Promotion rule

A new package enters `libs/go` only when at least two concrete consumers share
a stable mechanism, the boundary is domain-neutral, conformance tests exist,
and the dependency direction remains valid. Generic `common`, `shared`,
`helpers`, or `utils` packages are forbidden.
