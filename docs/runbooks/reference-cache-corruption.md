# Runbook: reference database cache corruption

## Trigger

A node-local MSA/template/reference database shard, index, or activation
manifest fails digest/format verification or produces inconsistent search
results.

## Immediate actions

1. Remove the cache snapshot from service readiness and stop scheduling searches
   that require it.
2. Preserve snapshot ID, shard digest, local path, filesystem/mount state, tool
   and index version, and verification diagnostics.
3. Do not mutate the promoted reference snapshot or mark local bytes valid
   manually.

## Recovery

- Delete/quarantine only the affected local cache generation.
- Re-download missing shards through the Rust object-store path, verify every
  digest, and atomically activate the complete snapshot.
- If durable bytes fail verification, quarantine the promoted snapshot and
  follow `artifact-corruption.md`.
- Run a deterministic search smoke corpus before returning the cache to ready.

## Exit criteria

All required shards and indexes match the immutable snapshot manifest, tool
compatibility is verified, activation is atomic/read-only, and the search smoke
corpus matches its approved result digests.
