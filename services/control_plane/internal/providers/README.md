# Control-plane provider construction

This package is where the control plane names concrete infrastructure. Every
other service package speaks to a `libs/go` contract; only this one knows that
audit is PostgreSQL, that artifacts are Google Cloud Storage objects, and that
the read cache is Redis.

```text
config.Settings
    -> mechanisms   (clock, identifiers, observability, retry, signing, pagination)
    -> database     (pool, migrations, transactions, audit, idempotency, outbox)
    -> blob store   (Google Cloud Storage)
    -> cache store  (Redis)
    -> identity     (service API keys, permission authorization)
    -> serving      (listener, canonical middleware stack, health)
    -> foundation.Dependencies + bootstrap.Components
```

## Materialized roles

| Role | Factory | Providers |
|---|---|---|
| `registry` | `NewRegistryFactory` | PostgreSQL, Google Cloud Storage, Redis |
| `event-dispatcher` | `NewEventDispatcherFactory` | PostgreSQL, broker |
| `scheduler` | `NewSchedulerFactory` | PostgreSQL, broker, Kubernetes |
| `controller` | `NewControllerFactory` | PostgreSQL, broker, Kubernetes + manager |
| `event-projector` | `NewProjectorFactory` | PostgreSQL, broker subscription |
| `api` | `NewAPIFactory` | PostgreSQL, HTTP + Connect + gRPC |
| `webhook-dispatcher` | `NewWebhookFactory` | PostgreSQL, broker, outbound HTTP |
| `operator` | `controller.NewOperatorFactory` | PostgreSQL, broker, Kubernetes + manager |
| `admin` | `api.NewAdminFactory` | PostgreSQL, HTTP + Connect + gRPC |
| `ingestion-controller` | `NewIngestionFactory` | PostgreSQL, GCS, Redis, broker, Kubernetes |
| `maintenance` | `NewMaintenanceFactory` | PostgreSQL |

## Shared provider packages

Providers used by more than one role live in a sibling package rather than in
this one, so a role links only the adapters it uses. Putting them here would
mean the dispatcher's binary carried a lease adapter it never opens.

```text
providers/cluster   Kubernetes REST config, client, discovery
providers/durable   audit, idempotency, lease, work-queue, cursor adapters
providers/broker    messaging provider resolution
providers/apikeys   service-to-service credential registry
providers/objects   artifact store and read cache
```

The root package keeps what every role needs: the pure mechanisms, the
PostgreSQL pool, and the table names those adapters are bound to.

The scheduler is the first role to hold a singleton lease and the first to
reach a cluster. Its placement handler is a seam rather than a stub: with none
injected the worker fails items closed, because a scheduler that acknowledges
work it cannot place loses it silently.

The event projector is the only role that holds the projector mechanism, and
the first to hold the inbox and cursors. Its event source and handler are one
injected seam: what the event log is, and what applying an event does, are
domain decisions. Unset, both fail closed — a projector reading an empty log
looks healthy while projecting nothing, and that is the failure that takes
longest to notice.

The api role is the only one that mounts Connect and gRPC, so every Connect
and gRPC submodule reaches production through it. It deliberately does not wrap
its mux in `httpx/otel`: the Connect handlers are already instrumented by
`connectx/otel`, and otelhttp would emit a second span for every Connect
request.

The webhook dispatcher is the only role that calls systems this control plane
does not own. Its egress allow-list is required rather than defaulted open --
a dispatcher that will call any host it is handed is a server-side request
forgery primitive with a queue in front of it -- and HTTPS and the ban on
private addresses are fixed rather than configurable.

The ingestion coordinator is the widest role: artifacts, a read cache, a
cursor, a work queue, and a cluster client at once. It holds a cursor without a
projector, because it tracks its position in each source it reads but does not
project an ordered event stream.

Maintenance is the narrowest role that still holds a lease. It runs no
migration runner despite `CONSUMPTION.md` naming it the intended migration
process: the registry owns the manifest today, and two runners against one
database would race for the same version ordering. Moving that ownership is a
deployment change, not a composition change.

The admin role shares the api factory, and the operator shares the controller's. The two roles have identical
capability profiles, so they are one composition claiming a different lease and
reporting events under a different source, not two implementations.

The controller adds the controller-runtime manager, and switches off three of
its subsystems that the foundation already owns: leader election belongs to
`coordination/leadership`, metrics to `observability`, and health probes to
`servicekit`. Running both would mean two answers to the same question — a
process can be leader by one lease and not the other. Its reconcilers are
registered by domain code; the composition root owns the manager's lifetime.

Every role is materialized. `bootstrap.UnconfiguredFactory` remains for a role
added before its providers exist: its command starts, validates its profile,
and refuses to run with exit 78. `--describe-profile` works either way, because
deployment tooling needs the manifest before the adapters do.

## Rules

- No in-memory adapter is reachable from this package, with one bounded
  exception: the messaging provider, because no Pub/Sub SDK is in `go.mod`.
  It is refused outside development or test by two independent gates. Do not
  extend the pattern to another store.
- Construction is ordered cheapest-first. Configuration and pure mechanisms
  fail before a socket, connection, or cloud client is opened, and anything
  already opened is released when a later step fails.
- No domain policy: no repositories, route tables, generated handlers, or
  business services are assembled here.
- Missing provider configuration is a startup failure, never a silent
  downgrade to a weaker adapter.

## Configuration

`registry` reads `MINDCLADE_DATABASE_DSN`, `MINDCLADE_BLOB_BUCKET`,
`MINDCLADE_CACHE_ADDRESS`, `MINDCLADE_SIGNING_HMAC_KEY`, and
`MINDCLADE_AUTH_API_KEYS`; see `internal/config` for the full schema and the
exact environment mapping. The API-key registry has the form

```text
subject:sha256hex:permission[,permission][;subject:sha256hex:permission...]
```

and carries service identity only. It is not a user-authentication mechanism
and must not grow into one.
