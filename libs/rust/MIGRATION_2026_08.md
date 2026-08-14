# Rust runtime consolidation epoch — 2026-08

The uploaded 21-crate foundation is the source baseline. Stable implementations
are migrated, not rewritten: `clock`/`retry`/`resource_version` -> `runtime_core`,
`byte_spec` -> `bytes_io`, `artifact_manifest` -> `manifests`, `observability` ->
`telemetry`, and `python_bindings` -> `python_bridge`. The old narrow crates are
kept for one compatibility epoch but are forbidden to new production consumers.
The broad `common` compatibility crate is removed immediately.

New production crates add `bounded_parse`, `bio_formats`, `worker_protocol`,
`worker_runtime`, and `gpu_host`. This keeps mechanisms cohesive without
splitting every operation into its own crate.
