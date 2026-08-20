# Python worker runtime contracts

This package is the Python scientific-engine side of the unified stage-worker protocol. The
wire representation is Protobuf under `protocols/proto/mindclade/orchestration/v1`; Rust
validates signed execution authority, fencing, resource budgets, deadlines, and bulk-buffer
descriptors before Python is invoked.

The package owns bounded, immutable process-local DTO validation and delegation to a
`StageEngine`. It re-exports the canonical location-independent
`libs.python.identifiers.ArtifactRef`; `schema_version` is therefore the wire-compatible
positive `uint32`, not the obsolete string form.

## Execution behavior

`StageExecutor.execute` validates the envelope, executor kind, and injected clock before
calling the numerical engine. An already-expired stage raises `DeadlineExceeded`; a kind
mismatch or invalid engine result raises `FailedPrecondition`. Engine exceptions propagate
unchanged so domain-specific fault classification is not erased.

Envelopes enforce canonical typed resource IDs, SHA-256 digests, positive wire-width counters,
bounded text, at most 4096 input/output references, 128 metadata fields, and 256 finite numeric
metrics. Metadata and metrics are copied into read-only mappings at construction.

This package does not import PyTorch, a broker, Kubernetes, a database, signing code, or
provider SDKs. Cancellation and distributed admission remain upstream responsibilities;
scientific algorithm behavior remains with each owning worker engine.
