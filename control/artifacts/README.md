# Artifacts

## Owns

Artifact catalog metadata, tenant grants, retention intent, and immutable reference policy.

## Does not own

Artifact byte streaming, digest implementation, or object-store transfer loops.

## Foundation consumption

`faults, identifiers`

This package is domain policy only: it defines the catalog contract, the
permanent digest/metadata binding, grant validity, and retention intent. It
holds no connection, transaction, or clock of its own, and `NewMemoryCatalog`
is the reference implementation the durable store is conformance-tested
against -- not a production store.

Durability lives in `services/control_plane/internal/store/postgres`, which
implements `Catalog` against the caller's transaction, so a registration
commits or rolls back with the unit of work containing it. `Register` is one
statement: the identity write and its placements share a single commit
boundary, because a half-registered artifact is permanent -- the digest
binding can never be rewritten to repair it. Errors are structured `faults`
carrying the reasons declared in `catalog.go`, IDs are canonical
`identifiers`, and process lifecycle never lives here.

Audit, outbox, and signing are not wired to this package. When a caller needs
them they belong in the same transaction as the catalog write, which is why
the store takes the caller's transaction rather than opening its own.
