# Admission

## Owns

Global admission, entitlements, quota budgets, reservations, and bounded execution authorization.

## Does not own

Local node/GPU admission or tensor memory estimation.

## Foundation consumption

`auth, audit, idempotency, resourceversion, storage/sql/transaction, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
