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

The implemented source includes canonical policy construction, signed immutable
snapshots, monotonic in-memory storage, and retry of the exact stored snapshot
after a delivery failure. The in-memory repository copies nested route and
signature data at both boundaries so a caller cannot mutate published policy.
It is a deterministic test/reference provider, not durable production storage.
Transactional SQL, audit/outbox publication, connected managed signing, and
runtime consumption remain qualification work in the owning service/provider
layers.
