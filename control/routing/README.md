# Routing

## Owns

Immutable deployment/route policy, canaries, rollout state, and route-snapshot publication.

## Does not own

Per-request network routing or response streaming.

## Foundation consumption

`resourceversion, signing, storage/sql/transaction, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
