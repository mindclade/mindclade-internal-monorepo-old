# Tenancy

## Owns

Organizations, projects, workspaces, service accounts, ownership, and tenant-scoped policy roots.

## Does not own

Identity-provider implementation or transport middleware.

## Foundation consumption

`auth, audit, idempotency, pagination, resourceversion, storage/sql/transaction`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
