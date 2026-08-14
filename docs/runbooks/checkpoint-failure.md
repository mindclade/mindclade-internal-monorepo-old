# Runbook: checkpoint failure

## Trigger

Checkpoint save, staging, upload, manifest commit, restore, reshard, or
verification fails or exceeds its pause/memory/transfer budget.

## Triage

1. Classify the phase: Python/DCP state planning, CPU staging, Rust transfer,
   object storage, manifest commit, registry publication, or restore.
2. Preserve run ID, step, topology fingerprint, checkpoint request ID, node/rank
   diagnostics, staging budget, and last successfully committed checkpoint.
3. Keep training on the last safe policy: continue without checkpoint only when
   the configured durability window permits it; otherwise stop at a safe point.
4. Never publish a checkpoint until all objects verify and the atomic manifest
   commit succeeds.

## Recovery

- Failed in-flight save: discard uncommitted staging objects and retry from the
  same safe training state if replay is supported.
- Host-memory pressure: reduce staging concurrency/chunk size or switch to the
  synchronous emergency policy; account for checkpoint bytes in the node-wide
  Rust budget.
- Transfer/provider failure: resume/retry verified parts with the transfer
  worker; do not make Python own provider retry loops.
- Restore failure: validate schema, component registry, topology compatibility,
  object digests, and reshard plan; fall back to the last verified checkpoint.
- Corrupt object: follow `artifact-corruption.md`.

## Exit criteria

A checkpoint is atomically committed and independently restorable, or the run is
terminated with the last verified checkpoint protected. Evidence includes
manifest, digests, topology, code/toolchain provenance, state registry, and a
restore qualification result.
