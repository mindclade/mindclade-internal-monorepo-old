# Serving / Batch

**Status:** implemented provider-neutral runtime; connected engines and deployment qualification pending.

This package owns durable batch-inference mechanics below Go orchestration and Rust ticket,
fencing, resource, process, and bulk-I/O enforcement. It provides validated immutable jobs,
stable resource-aware partitioning, a bounded earliest-deadline-first queue, cooperative
cancellation, deterministic retry classification, exact result cardinality, bounded model
caching, low-cardinality metrics, and canonical output-lineage manifests.

Model loading, tensor memory estimation, and execution are injected interfaces. This package
does not implement a scheduler, ticket verifier, artifact store, Kubernetes client, provider
SDK, or scientific model. Go durably schedules retries; `retry.classify` only returns the
decision and bounded delay. A result manifest is content-addressed but is not published until
the Rust artifact path verifies the current fencing token and atomically commits it.

Hard limits are explicit in `BatchLimits`. Admission rejects expired jobs, duplicate requests,
mixed bundle digests, oversized single requests, and full queues. Partitioning preserves input
order, and execution accepts only exactly one ordered result per request.
