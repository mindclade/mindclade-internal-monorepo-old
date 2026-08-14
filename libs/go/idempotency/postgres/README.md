# PostgreSQL idempotency store

This package is the production `idempotency.Store` adapter for Go APIs,
controllers, webhook receivers, ingestion coordinators, and durable workers.

The adapter uses row-level locking and compare-and-swap on the lease token and
record version. It detects a transaction in the context and participates in it;
otherwise it creates its own transaction for acquisition. This is the canonical
way to make request idempotency, domain writes, and transactional-outbox inserts
one atomic commit.

Apply the migration before constructing the store. Connected CI must run the
idempotency conformance suite against the pinned PostgreSQL version.
