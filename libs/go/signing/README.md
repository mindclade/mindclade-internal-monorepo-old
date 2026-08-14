# Detached signing

`signing` provides detached Ed25519 and HMAC-SHA-256 mechanics, canonical key
IDs, validity windows, algorithm allowlists, keyset snapshots, rotation overlap,
text/JSON encodings, and safe structured failures.

Domain packages define claims and canonical bytes. Use it for execution tickets,
route snapshots, signed pagination cursors, webhook payloads, and selected
manifests. Do not place tenant authorization, ticket budgets, revocation policy,
or route meaning in this package.
