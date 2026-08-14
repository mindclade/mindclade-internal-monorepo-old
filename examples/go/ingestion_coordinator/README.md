# Ingestion coordinator foundation integration

This executable is a runnable local reference for a durable ingestion control
pipeline. It composes real in-memory implementations of the shared mechanisms:

```text
immutable source snapshot work item
    -> fenced work-queue claim
    -> domain handler
    -> durable outbox append
    -> outbox dispatcher
    -> broker-neutral message
    -> bounded subscriber
```

It additionally wires strict configuration, process-local observability,
leadership leases, blob/cache/cursor stores, canonical request metadata, role
validation, readiness, drain, and bounded reverse shutdown through
`servicekit/production`.

The handler only coordinates a source-snapshot event. Scientific parsing,
normalization, curation, MSA generation, template search, and model
featurization remain Python/Rust domain engines outside `libs/go`.

```bash
go run ./examples/go/ingestion_coordinator
```
