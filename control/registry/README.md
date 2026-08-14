# Registry

## Owns

Models, datasets, checkpoints, reference databases, deployments, releases, compatibility and promotion policy.

## Does not own

Artifact bytes or model implementation.

## Foundation consumption

`audit, idempotency, pagination, resourceversion, signing, storage/sql/transaction, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
