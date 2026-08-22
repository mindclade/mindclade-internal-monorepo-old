# Checkpoint registry

This package is the durable Go policy projection for committed training
checkpoints. It registers immutable manifest and commit `ArtifactRef`s,
content-seals catalog records, applies retention policy, and performs
optimistic lifecycle transitions.

## Authority and lifecycle

- Python `training/checkpointing` owns state names, DCP save/restore semantics,
  and the semantic checkpoint manifest.
- Rust and the artifact plane own bounded byte transfer, digest verification,
  durable placement, and the manifest-last commit artifact.
- This package accepts only an already committed projection. New records begin
  `committed` at resource version one and may become `protected`, `expired`, or
  `revoked` through declared transitions.
- Registration requires an injected checkpoint verifier to resolve the exact
  immutable manifest, commit, and component bytes and return facts derived from
  those typed payloads. A separate publication authority must match those facts
  to the current admitted stage attempt, fence, and exact active policy epoch;
  caller-authored record fields are never sufficient authority.
- Only digest-valid `committed` records inside retention and `protected`
  records are restorable. A missed expiry projection fails closed at read time.

## Bounds and failure behavior

The production policy defines schema-v1 manifest and commit bounds of 4 MiB and
1 MiB respectively, with retention between one hour and 365 days. Its active
policy epoch is intentionally unset, so constructing the policy alone cannot
activate publication.
Checkpoint attempts, fencing tokens, policy epochs, and resource versions must
be positive. Timestamps use canonical millisecond precision, and a newly
registered record must still be inside its retention window. IDs, digests,
artifact kinds, parent identity, and the canonical record seal are validated
before storage or restore.

Repository failures propagate unchanged for transport-level retry decisions.
Validation and policy faults are stable non-retryable invalid-argument errors.
The repository must enforce create-only IDs and compare-and-swap versions; it
must never overwrite checkpoint bytes or infer identity from a provider URI.

## Non-responsibilities

This package does not upload objects, interpret tensors, choose a restore or
reshard plan, schedule retention deletion, construct provider clients, or make
a scientific/model readiness claim. No production verifier or publication
authority is composed yet. Before activation, current-fence authorization and
create-only storage must share one transactional/CAS authority (or be delegated
to the authoritative fenced terminal committer) so a fence cannot change
between validation and creation. Connected object-store durability,
concurrent-writer fencing, garbage-collection, and disaster-recovery evidence
remain separate qualification gates.

Focused validation is owned by
`//control/registry/checkpoints:checkpoints_test`.
