# Runtime Authority

## Owns

Canonical admission grants, execution tickets, artifact grants, route snapshots, keys, revocation epochs, and fencing claims.

## Does not own

Signature primitives or Rust enforcement.

## Foundation consumption

`signing, identifiers, auth, audit, resourceversion`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.

## Managed signing

The domain includes `RemoteEd25519Signer`, a narrow managed-asymmetric-signing
seam. Go remains the issuer/policy authority and sends the exact canonical MCCE1
claim bytes to a provider adapter. The adapter returns only the detached
Ed25519 signature and exposes no private key material to the runtime domain.

Production provider construction belongs in `services/control_plane`; Rust
runtime services consume bounded public verification keysets and validate
signed authority locally without a synchronous callback to Go.
