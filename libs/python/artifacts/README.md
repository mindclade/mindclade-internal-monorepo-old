# Python artifact validation

This package owns bounded, process-local validation of artifact bytes and manifests. It builds
canonical location-independent `ArtifactRef` values, verifies streamed chunks against declared
size and SHA-256 digest, validates immutable schema-v1 manifests, and computes deterministic
lineage order. `VerifiedArtifactClient` accepts an injected reader; no provider client or global
configuration is constructed here.

Limits are enforced before or during work: in-memory references are capped at 64 MiB, verification
accepts at most 1,000,000 chunks, manifests accept at most 256 parents and 64 annotations, and
lineage walks accept at most 4,096 nodes and 65,536 edges. Verification supports a cooperative
cancellation predicate. Invalid contracts raise structured Mindclade faults; bytes are never
returned until their size and digest match.

Go remains authoritative for artifact catalog and tenant policy, Rust for bulk transfer and
artifact-byte handling. This package does not own URIs, buckets, credentials, retention, retries,
catalog mutation, networking, or admission policy. Cross-process schema evolution belongs under
`protocols/`; the Python manifest is a process-local projection whose `schema_version` must be 1.
