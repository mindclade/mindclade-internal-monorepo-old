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

The scaffold commands currently pass `bootstrap.UnconfiguredFactory` and fail
closed. This is deliberate: a generated command cannot be deployed until its
service-owned factory constructs real provider adapters, repositories, domain
engines, and transports. Replacing that factory is the vertical implementation
point for each service.

## Process roles

| Command | Role | Core reusable mechanisms |
|---|---|---|
| `api` | API | auth, audit, idempotency, transactions, outbox store, HTTP/Connect/gRPC |
| `admin` | Admin | privileged auth, audit, idempotency, outbox store, transports |
| `registry` | Registry | blob/cache, audit, idempotency, outbox store, transports |
| `scheduler` | Scheduler | leases, leadership, Kubernetes, work queue, outbox |
| `controller` | Controller | leases, leadership, Kubernetes, work queue, outbox |
| `operator` | Operator | controller mechanisms plus Kubernetes reconciliation |
| `ingestion_controller` | Ingestion coordinator | blob/cache, cursor, leases, work queue, outbox, Kubernetes |
| `event_projector` | Event projector | inbox, idempotency, cursor, leadership, projector loop |
| `event_dispatcher` | Dispatcher | outbox store and fenced dispatcher loop |
| `webhook_dispatcher` | Webhook dispatcher | idempotency, work queue, audit, outbox |

All roles also consume clock, identifiers, request metadata, structured faults,
retry, observability, `servicekit`, and `servicekit/production`.

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

## Enforcement

`tests/integration/go_foundation/consumption_test.py` verifies that all commands
use the shared bootstrap, the bootstrap delegates to the production builder,
public foundation packages are assigned to concrete process roles, and no
catch-all `common`, `helpers`, `shared`, or `utils` package appears under
`libs/go`.
