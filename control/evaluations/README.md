# Evaluations

## Owns

Evaluation requests, lifecycle state, qualification references, and promotion-relevant outcomes.

## Does not own

Metric/numerical evaluation implementation.

## Foundation consumption

`idempotency, resourceversion, coordination/workqueue, coordination/outbox, storage/sql/transaction`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
