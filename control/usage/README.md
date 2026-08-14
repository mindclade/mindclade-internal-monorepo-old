# Usage

## Owns

Meter records, aggregation, attribution, reconciliation, and quota-accounting inputs.

## Does not own

Runtime telemetry transport or billing-provider implementation.

## Foundation consumption

`coordination/inbox, pagination, storage/sql/transaction, requestmeta`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
