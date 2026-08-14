# Orchestration

## Owns

Workflow compilation, dependencies, stage/attempt state machines, cancellation, leases, and reconciliation decisions.

## Does not own

Kubernetes client mechanics or Python/Rust task execution.

## Foundation consumption

`coordination/workqueue, storage/lease, retry, resourceversion, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
