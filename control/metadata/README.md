# Metadata

## Owns

Durable run/build/evaluation metadata records and query contracts.

## Does not own

Domain-specific model numerics or byte storage.

## Foundation consumption

`pagination, resourceversion, storage/sql/transaction, coordination/inbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
