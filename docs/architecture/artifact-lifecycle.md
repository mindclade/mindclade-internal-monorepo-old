# Artifact lifecycle

Artifacts include raw source bytes, curated shards, preprocessed bundles,
checkpoints, model/runtime bundles, evaluation reports, diagnostics, and release
evidence.

## Separation of authority

Go owns artifact catalog metadata, tenant policy, lineage, retention intent, and
signed grants. Rust owns the content-addressed byte plane, range/multipart I/O,
digest verification, local caching, and atomic manifest publication.

## Lifecycle

```text
allocate scoped upload
  -> stream to staging with bounds
  -> compute/verify digest and size
  -> publish immutable object
  -> commit manifest/catalog transaction
  -> serve verified range reads or signed URLs
  -> retain, supersede, archive, or garbage-collect by policy
```

An object digest never changes. Mutable names point to immutable manifests and
are guarded by resource-version preconditions.

## Security

Every operation is tenant-scoped and authorized by a short-lived grant limiting
allowed prefixes, operations, sizes, and expiry. Model weights and hidden
sets require stronger identity, audit, and environment attestation. Logs never
contain signed URLs, credentials, or arbitrary artifact contents.

## Integrity and repair

Reads verify metadata and may verify full/range digests according to manifest
policy. Corrupt cache entries are quarantined and refetched; corrupt durable
objects block dependent work and trigger the artifact-corruption runbook.
Repair creates evidence and never rewrites an object under the same digest.

## Garbage collection

GC considers catalog reachability, active leases, retention/legal holds,
checkpoint ancestry, release references, replication state, and a safety delay.
Deletion is idempotent, audited, and produces a tombstone/evidence record.
