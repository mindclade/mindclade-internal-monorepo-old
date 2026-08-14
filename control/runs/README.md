# Runs

## Owns

Durable runs, jobs, lifecycle transitions, cancellation intent, and client-visible state.

## Does not own

Trainer/model execution or process supervision.

## Foundation consumption

`idempotency, pagination, resourceversion, coordination/workqueue, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
