# Events

## Owns

Domain event envelope policy, publication routing, subscriptions, and dispatcher application contracts.

## Does not own

Broker mechanics or service-local outbox algorithms.

## Foundation consumption

`messaging, coordination/outbox, coordination/inbox, retry, requestmeta`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
