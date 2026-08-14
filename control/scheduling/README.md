# Scheduling

## Owns

Fleet capacity, queues, priorities, fair share, placement, topology, reservations, and preemption policy.

## Does not own

Node-local admission or GPU tensor batching.

## Foundation consumption

`storage/lease, coordination/leadership, coordination/workqueue, kubernetes, retry, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
