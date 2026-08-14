# Lineage

## Owns

Artifact/dataset/model/run lineage graph semantics and query policy.

## Does not own

Raw telemetry or scientific transformation implementation.

## Foundation consumption

`identifiers, pagination, storage/sql/transaction, coordination/inbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
