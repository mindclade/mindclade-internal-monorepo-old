# Go integration examples

These examples are executable vertical slices over the fully implemented `libs/go` foundation. They use in-memory conformance adapters so they run without cloud credentials. Production wiring swaps in the PostgreSQL, Kubernetes, object-store, and broker adapters while preserving the same contracts and `servicekit` lifecycle.

- [`control_plane_api`](control_plane_api/README.md): HTTP API, audit, outbox, broker publication, and projection.
- [`ingestion_coordinator`](ingestion_coordinator/README.md): durable work queue, cursor fencing, raw artifact commit, and event publication.
