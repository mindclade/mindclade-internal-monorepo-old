# Go foundation integration examples

The repository includes three runnable integrations that use the implemented
foundation rather than pseudocode. All use deterministic local adapters and
preserve the same contracts used by production providers.

## Control-plane API

Path: `examples/go/control_plane_api`

```text
bounded HTTP request
    -> request metadata and canonical IDs
    -> structured validation/faults
    -> audit event
    -> transactional-outbox-shaped append
    -> broker publication and bounded projector
    -> service lifecycle and shutdown
```

Run it with:

```bash
go run ./examples/go/control_plane_api/cmd/control-plane-api
curl -sS -X POST http://127.0.0.1:8080/v1/runs \
  -H 'content-type: application/json' \
  -d '{"name":"novafold-evaluation"}'
```

This is a local integration of `httpx`, `requestmeta`, `identifiers`, `audit`,
`coordination/outbox`, `messaging`, and lifecycle handling. The production
control-plane command uses the stricter role-capability path under
`services/control_plane/internal/bootstrap` and `servicekit/production`.

## Durable event dispatcher

Path: `examples/go/event_dispatcher`

```text
domain outbox append
    -> fenced outbox claim
    -> broker-neutral publish adapter
    -> bounded in-memory subscription
    -> outbox published transition
    -> servicekit drain and reverse shutdown
```

Run it with:

```bash
go run ./examples/go/event_dispatcher
```

This example uses `servicekit/production.Builder` with the stable dispatcher
role. It demonstrates that a process cannot build until the required clock,
configuration, identifiers, request metadata, retry, observability, database,
messaging, outbox-store, and outbox-dispatcher mechanisms are represented.

Production replaces the store and broker with PostgreSQL and Pub/Sub while
preserving the same coordination and lifecycle contracts.

## Ingestion coordinator

Path: `examples/go/ingestion_coordinator`

```text
fenced leadership
    -> leased ingestion item
    -> leader-gated handler
    -> monotonic source cursor
    -> outbox append
    -> work completion
    -> deterministic shutdown
```

Run it with:

```bash
go run ./examples/go/ingestion_coordinator
```

The outbox record deliberately remains pending. Publication is owned by the
separate dispatcher process, preventing ingestion workflow code from coupling
durable state changes to broker availability.

The production ingestion service extends this slice with PostgreSQL
transactions, audit, idempotency, blob/cache providers, Kubernetes admission,
source-specific domain logic under `control/ingestion`, Rust transfer/parser
workers, and Python curation stages.

## Tests

```bash
go test -race ./examples/go/control_plane_api/...
go test -race ./examples/go/event_dispatcher ./examples/go/ingestion_coordinator
```

Memory adapters are never implicit production fallbacks. A promoted service
factory must wire the qualified provider adapters and pass its role capability
manifest before startup.
