# Artifacts

## Owns

Artifact catalog metadata, tenant grants, retention intent, and immutable reference policy.

## Does not own

Artifact byte streaming, digest implementation, or object-store transfer loops.

## Foundation consumption

`auth, audit, identifiers, resourceversion, signing, storage/sql/transaction, coordination/outbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.
