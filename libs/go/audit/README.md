# audit

`audit` defines immutable, schema-versioned security and control-plane audit
events plus a minimal `Recorder` contract.

Events contain actor and target snapshots, request lineage, terminal outcome,
stable reason codes, optional before/after digests, and bounded non-sensitive
fields. Raw request bodies, credentials, biological datasets, and arbitrary
before/after values must never be embedded.

Persistence, publication, retention, indexing, and delivery guarantees belong
to recorder adapters outside this package.
