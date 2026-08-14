# Webhooks

## Owns

Endpoints, subscriptions, event filtering, signing policy, delivery state, and retry/dead-letter decisions.

## Does not own

Generic outbound HTTP hardening or signature algorithms.

## Foundation consumption

`httpx/outbound, signing, coordination/workqueue, coordination/inbox, audit`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
