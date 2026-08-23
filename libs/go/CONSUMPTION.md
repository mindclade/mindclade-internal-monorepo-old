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

Columns are the process roles declared in
`services/control_plane/internal/bootstrap/profile.go`. Roles share a column
only when their generated `libs/go` inventories are identical, so a column
never averages roles that differ: `api` and `admin` link the same set because
`admin` reuses the api factory, and `controller` and `operator` link the same
set because `operator` reuses the controller factory. `registry`,
`event-dispatcher`, and `webhook-dispatcher` get their own columns because they
do not — grouping `event-dispatcher` with `webhook-dispatcher` previously hid a
13-package difference and made this table assert `httpx/outbound` was
"required for webhooks" for a binary that links no HTTP client at all.

Every cell begins with one of five tokens. Four of them are checked against the
generated inventory by `tools/analysis/check_foundation_consumption.py`, so no
cell can claim a link the build does not have, or deny one it does.

| Token | Meaning | Checked |
|---|---|---|
| `required` | the role's inventory must contain the mechanism | fails if absent |
| `no` | the role's inventory must not contain it | fails if present |
| `transitive` | the role links it, but through a shared control-plane composition package rather than its own code | fails if absent |
| `unmaterialized` | intended, but no binary in this column links it today — debt, not a fact | fails if present, so it must be promoted to `required` when it lands |
| `optional` | discretionary; genuinely unchecked | — |

Two limits are worth stating rather than implying. `optional` is unchecked, so
demoting a cell to it is a ratchet move and must say in the commit what changed.
And `transitive` is enforced identically to `required` — both require the link —
so the "rather than its own code" half is editorial: flipping a cell between the
two is a claim a reviewer has to judge, not one the checker can.

| Mechanism | `api` `admin` | `registry` | `scheduler` | `controller` `operator` | `ingestion-controller` | `event-projector` | `event-dispatcher` | `webhook-dispatcher` | `maintenance` |
|---|---|---|---|---|---|---|---|---|---|
| `clock` | required | required | required | required | required | required | required | required | required |
| `config` | required | required | required | required | required | required | required | required | required |
| `faults` | required | required | required | required | required | required | required | required | required |
| `identifiers` | required | required | required | required | required | required | required | required | required |
| `requestmeta` | required | required | required | required | required | required | required | required | required |
| `observability` | required | required | required | required | required | required | required | required | required |
| `retry` | required | required | required | required | required | required | required | required | required |
| `servicekit` `servicekit/production` | required | required | required | required | required | required | required | required | required |
| `auth` | required | required | required (internal identity) | required (internal identity) | required (internal identity) | required (internal identity) | required (internal identity) | required (internal identity) | required (internal identity) |
| `audit` | required (mutations) | required (mutations) | required | required | required | optional (policy-dependent) | unmaterialized (delivery audit) | required (delivery audit) | required |
| `idempotency` | required (mutations) | required (mutations) | required | required | required | required (through inbox) | unmaterialized (delivery-specific) | required (delivery-specific) | required (where mutating) |
| `resourceversion` | required | required | unmaterialized | unmaterialized | unmaterialized | optional (projection-specific) | no | no | required (maintenance-specific) |
| `pagination` | required | required | transitive | transitive | transitive | transitive | transitive | transitive | transitive |
| `signing` | required (cursor tokens) | required (cursor tokens) | required (execution tickets) | optional | optional | optional (verification only) | optional | required (webhook signing) | optional |
| `messaging` | required (outbox only) | required (outbox only) | required | required | required | required (subscription) | required (publication) | required (publication) | optional |
| `storage/sql/postgres` `storage/sql/transaction` | required | required | required | required | required | required | required | required | required |
| `storage/sql/migrate` | transitive | transitive | transitive | transitive | transitive | transitive | transitive | transitive | required (migration process) |
| `storage/blob` | no | required (artifact references) | optional | optional | required | optional | optional | optional | optional |
| `storage/cache` | no | required (read paths) | optional | optional | required | optional | optional | optional | optional |
| `storage/lease` | optional | optional | required | required | required | required (fencing source) | optional | required (claim ownership) | required |
| `kubernetes` | optional (query/admin paths) | optional (query/admin paths) | required | required | required (job launch) | optional | no | no | optional |
| `coordination` | required | required | required | required | required | required | optional | required | required |
| `coordination/outbox` | required | required | required | required | required | transitive | required (consumer) | required (consumer) | optional |
| `coordination/inbox` | optional | optional | optional | optional | required | required | optional | optional | optional |
| `coordination/cursor` | optional | optional | optional | optional | required | required | optional | optional | optional |
| `coordination/workqueue` | required (enqueue) | required (enqueue) | required | required | required | optional | optional | optional | required |
| `coordination/leadership` | no | no | required | required | optional | optional (fence) | no | no | required |
| `coordination/projector` | no | no | no | no | no | required | no | no | no |
| `httpx` `connectx` `grpcx` | required (transport boundary) | required (transport boundary) | optional (admin) | optional (metrics/admin) | optional (admin) | optional (admin) | optional (admin) | optional (admin) | optional (admin) |
| `httpx/outbound` | optional (policy-bound integrations) | optional | optional | optional | optional (source-specific) | optional | no | required (webhooks) | optional (maintenance-specific) |

A package is attributed to the most specific row that matches it, so
`coordination/outbox/postgres` is governed by the `coordination/outbox` row and
not by the `coordination` row. Every `libs/go` package that any binary links
must be attributed to some row; `libs/go/internal/*` is exempt because it is
not a mechanism consumers may select. Layering is enforced separately by
`tools/analysis/check_go_layers.py`; Bazel visibility does not currently
constrain it, because nearly every package under `libs/go` declares
`//visibility:public`.

### The three `transitive` rows are a real finding, not a rounding error

`pagination`, `storage/sql/migrate`, and — for `event-projector` —
`coordination/outbox` are in every binary that the table used to say `no` for:

```text
cmd/scheduler -> providers/scheduler -> foundation/governance  -> libs/go/pagination
cmd/scheduler -> providers/scheduler -> foundation/persistence -> libs/go/storage/sql/migrate
cmd/event_projector -> providers/projector -> foundation/eventing -> libs/go/coordination/outbox
```

Every role's provider package imports `foundation/persistence`, so every
control-plane binary links the forward-only schema-migration engine, including
the ten roles that must never run a migration. Nothing detected this because
the table was hand-written and unchecked while `consumption.json` was generated
and unread. The cells now say `transitive` rather than `no`, which is the truth
about the link graph; narrowing `foundation/persistence` so that only the
maintenance role links `storage/sql/migrate` is a change in
`services/control_plane/internal/foundation/`, not in this document.

## What is actually consumed

The table above is intent. The record of what each process really links is
generated from the Go import graph and cannot be written by hand:

| Artifact | Meaning |
|---|---|
| `services/control_plane/internal/bootstrap/consumption.json` | Per-role transitive `libs/go` inventory, generated from imports and embedded so `--describe-profile` reports what the binary links |
| `libs/go/UNCONSUMED.toml` | Every `libs/go` package with no in-module importer, with the reason it is still here |
| `tools/analysis/check_foundation_consumption.py` | Presubmit check that regenerates `consumption.json`, recomputes the unconsumed set, and fails when either — or the matrix above — disagrees with the import graph |

Regenerate after changing what a command imports:

```bash
python3 tools/analysis/check_foundation_consumption.py --write
```

`--write` regenerates `consumption.json` only. `UNCONSUMED.toml` carries a
human reason per waiver and is never machine-written; the checker recomputes
the unconsumed set and tells you which waivers to add or delete.

The inventory is a **transitive** closure of each command package, so a
mechanism appears in it whenever anything the command reaches imports it — that
is why a role can link a package its own code never names.

All 11 declared roles are materialized. `bootstrap.UnconfiguredFactory` is
retained for a role added before its providers exist, and fails closed when
reached; the checker also fails a role that is declared in `profile.go` with no
command directory under `services/control_plane/cmd/`.

Materializing a role is what validates the foundation it links, so the order
matters. `scheduler` was taken first because it lights up the largest unlinked
block — the Kubernetes tree, the leased work queue, singleton leadership, and
the PostgreSQL lease adapter. `controller` followed because it adds no new
packages and instead gives that block a second consumer.

The leased work queue and singleton leadership did reach independent consumers
that way: `coordination/workqueue` is imported by five provider packages plus
`foundation/tasks`, and `coordination/leadership` by five provider packages
plus `foundation/leasing`. **The Kubernetes tree did not.** Of its eleven
packages, five (`kubernetes`, `kubernetes/client`, `kubernetes/controller`,
`kubernetes/events`, `kubernetes/metadata`) appear in any binary's inventory,
and six (`conditions`, `finalizers`, `ownerrefs`, `patch`, `status`, `watch`)
are reachable only from the single test package
`tests/integration/kubernetes_foundation` — `conditions` and `patch` only
through `status`, which itself has no production importer. Of the five that are
linked, `kubernetes/client` and `kubernetes/controller` each have exactly one
in-repo importer (`internal/providers/cluster` and
`internal/providers/controller`); the four roles that link them do so through
that one package. A single integration test and a single provider package are
not the two independent production consumers `ADMISSION.md` asks for, so the
Kubernetes tree is linked and test-exercised, not admitted.

`event-projector` followed, lighting the last large unlinked block: the
projector loop, the idempotent inbox, and compare-and-advance cursors. It is
the sole intended consumer of `coordination/projector`, which is why that
package cannot reach the two-consumer bar and is a permanent single-consumer
exemption rather than ordinary debt.

`api` followed, retiring the whole Connect and gRPC waiver block, and
`webhook-dispatcher` claimed `httpx/outbound`, its only consumer.
`operator` reuses the controller factory and `admin` reuses the api factory,
which is what gives the Kubernetes and transport trees further roles at no new
package cost — further roles, not further importers. `ingestion-controller` and
`maintenance` closed the fleet.

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
