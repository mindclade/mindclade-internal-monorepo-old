# Weights

## Owns

Model-weight access policy, grants, approvals, receipts, revocation, and audit.

## Does not own

Weight byte storage or model loading.

## Foundation consumption

`auth, audit, signing, resourceversion, storage/sql/transaction, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
