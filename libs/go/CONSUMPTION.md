# Repository-wide Go foundation consumption

Copyright 2026 Mindclade. All rights reserved. Confidential and proprietary.

The Go foundation is designed for broad consumption without becoming a generic
platform namespace. It owns stable mechanisms that would otherwise be
reimplemented by control-plane services; consumers own domain policy,
repositories, generated APIs, reconciliation decisions, and executable wiring.

## Mandatory process path

Every production Go executable uses:

```text
service-specific provider construction
    -> servicekit/production.Builder
    -> validated role capability profile
    -> staged servicekit lifecycle
    -> bounded signal, drain, probe, and shutdown handling
```

Direct use of `servicekit.New`, ad hoc signal handling, detached goroutines,
process-local retry loops, or service-local health frameworks is prohibited in
production commands. Tests and low-level library conformance suites may use the
lower-level APIs directly.

## Process consumption matrix

| Mechanism | API / admin / registry | Scheduler | Controller / operator | Ingestion coordinator | Projector | Dispatcher / webhook | Maintenance |
|---|---:|---:|---:|---:|---:|---:|---:|
| `clock` | required | required | required | required | required | required | required |
| `config` | required | required | required | required | required | required | required |
| `faults` | required | required | required | required | required | required | required |
| `identifiers` | required | required | required | required | required | required | required |
| `requestmeta` | required | required | required | required | required | required | required |
| `observability` | required | required | required | required | required | required | required |
| `retry` | required | required | required | required | required | required | required |
| `servicekit` + `servicekit/production` | required | required | required | required | required | required | required |
| `auth` | required | internal identity | internal identity | internal identity | internal identity | internal identity | internal identity |
| `audit` | required for mutations | required | required | required | policy-dependent | delivery audit | required |
| `idempotency` | required for mutations | required | required | required | required through inbox | delivery-specific | required where mutating |
| `resourceversion` | required | required | required | required | projection-specific | no | maintenance-specific |
| `pagination` | required | no | no | no | no | no | no |
| `signing` | pagination/tickets | execution tickets | optional | optional | verification only | webhook signing | optional |
| `messaging` | outbox only | required where consuming/publishing | required | required | required subscription | required publication | optional |
| `storage/sql` | required | required | required | required | required | required | required |
| `storage/sql/migrate` | no | no | no | no | no | no | required migration process |
| `storage/blob` | registry/artifact references | optional | optional | required | optional | optional | optional |
| `storage/cache` | registry/read paths | optional | optional | required | optional | optional | optional |
| `storage/lease` | optional | required | required | required | fencing source | claim ownership | required |
| `kubernetes` | query/admin paths | required | required | job launch | optional | no | optional |
| `coordination/outbox` | required | required | required | required | no | required consumer | optional |
| `coordination/inbox` | optional consumers | optional | optional | optional | required | optional | optional |
| `coordination/cursor` | optional | optional | optional | required | required | optional | optional |
| `coordination/workqueue` | enqueue | required | required | required | optional | optional | required |
| `coordination/leadership` | no | required | required | optional | optional fence | optional | required |
| `coordination/projector` | no | no | no | no | required | no | no |
| `httpx` / `connectx` / `grpcx` | required transport boundary | optional admin | optional metrics/admin | optional admin | optional admin | optional admin | optional admin |
| `httpx/outbound` | policy-bound integrations | optional | optional | source-specific | optional | required for webhooks | maintenance-specific |

The table expresses **intended** repository consumption, not permission for
every package to import every other package, and not a record of what any
binary links today. Layering is enforced by
`tools/analysis/check_go_layers.py`; Bazel visibility does not currently
constrain it, because nearly every package under `libs/go` declares
`//visibility:public`.

## What is actually consumed

The table above is a target. The record of what each process really links is
generated from the Go import graph and cannot be written by hand:

| Artifact | Meaning |
|---|---|
| `services/control_plane/internal/bootstrap/consumption.json` | Per-role transitive `libs/go` inventory, generated from imports and embedded so `--describe-profile` reports what the binary links |
| `libs/go/UNCONSUMED.toml` | Every `libs/go` package with no in-module importer, with the reason it is still here |
| `tools/analysis/check_foundation_consumption.py` | Presubmit check that regenerates both views and fails on drift |

Regenerate after changing what a command imports:

```bash
python3 tools/analysis/check_foundation_consumption.py --write
```

A package that appears in the table above but not in `consumption.json` is not
consumed yet — the role that would consume it has no materialized provider
factory. Eight of the twelve roles are materialized: `registry`, `event-dispatcher`,
`scheduler`, `controller`, `operator`, `event-projector`, `api`, and
`webhook-dispatcher`. `admin`, `ingestion-controller`, and `maintenance` still
bootstrap through `bootstrap.UnconfiguredFactory` and fail closed.

Materializing a role is what validates the foundation it links, so the order
matters. `scheduler` was taken first because it lights up the largest unlinked
block — the Kubernetes tree, the leased work queue, singleton leadership, and
the PostgreSQL lease adapter. `controller` followed because it adds no new packages
and instead gives that block its second independent consumer, which is what
`ADMISSION.md` actually asks for. The Kubernetes tree, the leased work queue,
and singleton leadership are now admitted rather than merely linked.

`event-projector` followed, lighting the last large unlinked block: the
projector loop, the idempotent inbox, and compare-and-advance cursors. It is
the sole intended consumer of `coordination/projector`, which is why that
package cannot reach the two-consumer bar and is a permanent single-consumer
exemption rather than ordinary debt.

`api` followed, retiring the whole Connect and gRPC waiver block, and
`webhook-dispatcher` claimed `httpx/outbound`, its only required consumer.
`operator` reuses the controller factory, which is what gives the Kubernetes
tree its third consumer at no new package cost.

## Canonical mechanisms

### Durable mutation

```text
SQL transaction
    -> domain repository mutation
    -> audit/postgres recorder using the same transaction
    -> coordination/outbox insert using the same transaction
    -> commit
    -> outbox dispatcher publishes asynchronously
```

There is no service-specific outbox table, dispatcher loop, retry policy, or
claim algorithm.

### Durable event consumption and projection

```text
received event
    -> coordination/inbox idempotent transaction
    -> projector handler effects
    -> coordination/cursor compare-and-advance
    -> commit
```

The inbox reuses the existing `idempotency` contracts. Projectors do not invent
a second deduplication model.

### Durable background work

```text
producer enqueues immutable work item
    -> coordination/workqueue claim with fencing token
    -> bounded worker concurrency
    -> heartbeat lease renewal
    -> claim-loss cancellation
    -> completion, retry, or dead-letter transition
```

Schedulers, controllers, operators, ingestion coordinators, and maintenance
processes share this mechanism while retaining domain-specific handlers.

### Singleton authority

`coordination/leadership` adapts the canonical `storage/lease` contract into a
service-managed elector with fencing and bounded renewal. Domain code receives
leadership state; it does not own election goroutines.

## Provider adapters

Use exactly one production adapter per provider capability:

```text
PostgreSQL lifecycle       storage/sql/postgres.Pool
SQL transaction context   storage/sql/transaction
Schema migrations          storage/sql/migrate
PostgreSQL audit           audit/postgres
PostgreSQL idempotency     idempotency/postgres
PostgreSQL durable queues  coordination/*/postgres
Blob storage               storage/blob/{gcs,memory}
Cache                      storage/cache/{redis,memory}
Lease                      storage/lease/{postgres,memory}
Kubernetes                 kubernetes/*
Messaging                  messaging + messaging/pubsub provider adapter
Configuration              config
Optimistic concurrency     resourceversion
Cursor signing             pagination + signing
HTTP                       httpx
Connect                    connectx
gRPC                       grpcx
```

Provider-specific configuration belongs in the service composition root.
Provider clients do not leak into domain packages when a stable foundation
contract exists.

## Package-consumption rules

1. Import the narrowest package that owns the mechanism.
2. Do not create `common`, `shared`, `helpers`, `utils`, or generic repository
   packages inside `libs/go`.
3. Do not add a domain abstraction merely to increase reuse. A new shared
   package must eliminate duplicate mechanism code in at least two concrete
   consumers and have conformance tests.
4. Do not hide transactions. APIs that must atomically mutate state, audit, and
   enqueue an event accept the explicit transaction context.
5. Do not publish directly from request transactions. Use the outbox.
6. Do not perform long-running work in an outbox dispatcher. Use work queues.
7. Do not use work queues for ordered event projection. Use inbox + cursor.
8. Do not let lower layers import transports, providers, or executable packages.
9. Test-only adapters remain `testonly` Bazel targets and are forbidden from
   production dependencies.
10. Every process role is statically checked in repository qualification and
    dynamically validated by `servicekit/production` before startup.
