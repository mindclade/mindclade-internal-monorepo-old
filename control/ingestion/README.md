# Ingestion control domain

`control/ingestion` owns durable source identities, immutable upstream snapshot records, monotonic provider cursors, ingestion-only stage plans, and compilation of a stage attempt into the canonical cross-language `orchestration.WorkloadEnvelope`.

It deliberately does **not** parse biological formats, normalize scientific records, execute downloads/search tools, or construct model features. Python owns scientific semantics and Rust owns byte movement/node execution.

Production persistence and source-specific adapters remain service-owned; the domain package is provider-neutral and consumes `control/artifacts`, `control/orchestration`, `control/runtime_authority`, and the Go foundation.
