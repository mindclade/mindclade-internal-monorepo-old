# Audit

## Owns

Queryable durable audit policy and records for control-plane actions.

## Does not own

Generic audit event construction, which is in libs/go/audit.

## Foundation consumption

`audit, audit/postgres, pagination, storage/sql/transaction`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
