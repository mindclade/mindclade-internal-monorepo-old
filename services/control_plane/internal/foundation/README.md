# Control-plane process foundation

This package is the typed composition boundary between reusable `libs/go`
mechanisms and concrete Go processes. It is intentionally **not** a shared
business-domain library.

It centralizes only:

- process-wide dependencies and capability validation;
- canonical `servicekit` options;
- lifecycle adapters already exposed by `observability`, PostgreSQL,
  leadership, outbox, projector, and workqueue packages;
- deterministic component naming and ordering.

Tenancy, admission, routing, ingestion, scheduling, registry, usage, webhook,
and administrative policy stay under `control/` and service adapters.
