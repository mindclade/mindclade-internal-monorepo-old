# Durable coordination

`coordination` contains fenced, bounded, durable execution mechanisms shared by
control-plane processes. It is deliberately not a business-domain layer.

Allowed responsibilities:

- transactional event publication;
- idempotent event consumption;
- leased work claims and retries;
- projection checkpoints;
- leader leases and fencing;
- service-managed dispatch and drain behavior.

Prohibited responsibilities:

- tenant, quota, scheduling, model, dataset, or product policy;
- generic helpers or convenience wrappers;
- transport-specific request models;
- provider-specific storage code outside focused adapter subpackages.

## Implemented primitives

| Package | Shared mechanism |
|---|---|
| `cursor` | Fenced compare-and-swap source/projector positions |
| `inbox` | Transactional deduplication around `idempotency.Store` |
| `leadership` | Lease-backed leadership sessions and fencing |
| `outbox` | Transactional publication, claiming, retries, and dead letters |
| `projector` | Bounded inbox + cursor event projection loop |
| `workqueue` | Durable leased control-plane work with retries and terminal state |

Concrete domain events, workflow states, scheduling policy, ingestion semantics,
and registry behavior remain outside `libs/go`.
