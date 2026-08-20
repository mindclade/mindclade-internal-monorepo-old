# Go foundation consumption in the control plane

Every Go control-plane process uses the supplied `libs/go` foundation through a
single assembly path. The services own policy and provider configuration;
`libs/go` owns reusable mechanisms.

## Process path

```text
cmd/<process>/main.go
    -> internal/bootstrap.Main(role, service-owned factory)
    -> internal/foundation.Dependencies.Register
    -> servicekit/production.Builder
    -> validated role capability manifest
    -> staged servicekit lifecycle
    -> readiness, drain, signals, and bounded reverse shutdown
```

Every role is materialized: each command wires a service-owned factory that
constructs real provider adapters. `bootstrap.UnconfiguredFactory` remains for
a role added before its providers exist — it fails closed with exit 78 — and
`internal/bootstrap/promotion_test.go` enforces that no command reaches it.

The registry supplies concrete model-publication and release-promotion policy.
Roles listed under "Domain seams" still expose injectable handlers with
fail-closed defaults, so those processes assemble and validate but refuse work
until their own domain composition is supplied.

## Process roles

| Command | Role | Core reusable mechanisms |
|---|---|---|
| `api` | API | auth, audit, idempotency, transactions, outbox store, HTTP/Connect/gRPC |
| `admin` | Admin | the API composition on its own listeners |
| `registry` | Registry | model/release domains, PostgreSQL registry store, blob/cache, audit, idempotency, outbox store, HTTP; owns the migration manifest |
| `scheduler` | Scheduler | leases, leadership, Kubernetes, work queue, outbox |
| `controller` | Controller | leases, leadership, Kubernetes + manager, work queue, outbox |
| `operator` | Operator | the controller composition under its own lease and event source |
| `ingestion_controller` | Ingestion coordinator | blob/cache, cursor, leases, work queue, outbox, Kubernetes |
| `event_projector` | Event projector | inbox, idempotency, cursor, leadership, projector loop |
| `event_dispatcher` | Dispatcher | outbox store and fenced dispatcher loop |
| `webhook_dispatcher` | Webhook dispatcher | idempotency, work queue, audit, outbox, policy-bound outbound HTTP |
| `maintenance` | Maintenance | leases, leadership, work queue, audit |

All roles also consume clock, identifiers, request metadata, structured faults,
retry, observability, `servicekit`, and `servicekit/production`.

Two roles share a composition rather than duplicating one. `admin` uses the
`api` factory and `operator` uses the `controller` factory: each pair has an
identical capability profile, and they stay distinct processes because they are
separately deployed, separately addressed, and — for the operator — claim a
separate lease under a separate event source.

## Domain seams

Each composition root owns the mechanism and leaves the policy injectable. The
default in every case fails closed rather than reporting success, because a
process that silently does nothing is the failure that takes longest to notice.

| Role | Seam | Default fault reason |
|---|---|---|
| `scheduler` | `WithPlacementHandler` | `placement_handler_not_configured` |
| `event_projector` | `WithProjection` (source and handler) | `projection_source_not_configured` |
| `webhook_dispatcher` | `WithDeliveryHandler` | `delivery_handler_not_configured` |
| `ingestion_controller` | `WithStagingHandler` | `staging_handler_not_configured` |
| `maintenance` | `WithHousekeepingHandler` | `housekeeping_handler_not_configured` |

## Provider packages

Providers used by more than one role live beside the composition root rather
than inside it, so a role links only the adapters it opens.

```text
internal/providers            pure mechanisms, PostgreSQL pool, table names
internal/providers/durable    audit, idempotency, lease, work-queue, cursor
internal/providers/objects    artifact store and read cache
internal/providers/broker     messaging provider resolution
internal/providers/cluster    Kubernetes REST config, client, discovery
internal/providers/apikeys    service-to-service credential registry
```

The shared `internal/providers` package is mechanism-only. Role subpackages
under `internal/providers/<role>` are Layer-5 process composition roots: they
may bind reusable `control/` services to concrete repositories and transports.
This is the boundary used by the registry and is the standing rule for future
role materialization.

## Domain storage

`internal/store/postgres` implements the repository contracts the domain
declares — `control/registry/models.Repository` and
`control/registry/releases.Repository` — rather than contracts of its own. The
domain owns the seam; storage implements it, so the two cannot drift.

Concurrency control follows each record's own model rather than one policy
imposed across both. A `models.Descriptor` is content-addressed, since
`SealDigest` covers every field including `Lifecycle`, so no in-place update is
representable: writes are insert-if-absent, an identical republish is a no-op,
and a lifecycle change is a new row under a new digest. A `releases.Release`
carries its own `ResourceVersion`, so writes compare-and-swap on it; this is
deliberately not `libs/go/resourceversion`, because two version fields on one
record is two things to keep agreeing. An `EvidenceGraph` is sealed by the
digest its release quotes and is therefore immutable once written.

`internal/providers/registry.RegistryFactory` constructs the store and binds it
to `models.Service` and `releases.Service`. Release promotion wraps evidence and
release writes in one serializable transaction. The HTTP adapter depends only
on narrow domain interfaces, keeping provider selection out of reusable domain
packages and out of the command.

## Durable coordination contracts

### Mutation and publication

```text
SQL transaction
    -> domain mutation
    -> audit record
    -> outbox insert
    -> commit
    -> asynchronous dispatcher publication
```

### Projection

```text
event
    -> inbox/idempotency transaction
    -> projection effects
    -> fenced cursor advance
    -> commit
```

### Background work

```text
enqueue immutable item
    -> fenced claim
    -> bounded worker concurrency
    -> heartbeat renewal
    -> claim-loss cancellation
    -> complete, retry, or dead-letter
```

### Singleton ownership

`coordination/leadership` adapts `storage/lease` into a service-managed elector.
Domain code observes leadership; it does not own election goroutines.

The controller and operator run the controller-runtime manager with its own
leader election switched off. Singleton authority is the foundation lease, and
a process holding two elections can be leader by one and not the other.

## Enforcement

| Check | Enforces |
|---|---|
| `internal/bootstrap/promotion_test.go` | every role has a command, every command enters through `bootstrap.Main`, no command wires `UnconfiguredFactory` or builds a service directly |
| `internal/bootstrap/profile_test.go` | the consumption matrix is derived from the build, and no role links a provider adapter its profile does not justify |
| `tools/analysis/check_foundation_consumption.py` | `consumption.json` matches the real import graph, and every `libs/go` package has an importer or a waiver in `UNCONSUMED.toml` |
| `tools/analysis/check_go_layers.py` | the Layer 0–4 dependency law in `libs/go/LAYERS.md` |
