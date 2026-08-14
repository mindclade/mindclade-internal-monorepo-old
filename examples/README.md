# Integration examples

The examples in this directory are intentionally small, runnable vertical slices
that exercise the implemented Go foundation without pretending the broader
scientific scaffold is complete.

- `go/event_dispatcher` wires `servicekit/production`, the durable outbox, the
  in-memory broker, retry, identifiers, request metadata, and staged shutdown.
- `go/ingestion_coordinator` wires leadership, a fenced work queue, a monotonic
  cursor, outbox publication, and the production ingestion-coordinator profile.

The memory adapters are qualification fixtures. Production composition roots
replace them with PostgreSQL, Pub/Sub, Kubernetes, GCS, Redis, and other pinned
adapters while retaining the same contracts and lifecycle.
