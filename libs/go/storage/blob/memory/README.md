# blob/memory

Concurrency-safe in-memory implementation of `blob.Store`. It preserves
conditional generations and computes SHA-256 digests, but is not durable and
is bounded by a configurable per-object size.
