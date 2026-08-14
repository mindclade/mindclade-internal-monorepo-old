# Shared Rust Package Catalog

Production crates: **23**  
Compatibility crates: **7**

| Package | Layer | Status | Direct internal dependencies |
|---|---:|---|---|
| `bytes_io` | 0 | stable | `faults` |
| `content_digest` | 0 | stable | `faults` |
| `faults` | 0 | stable | — |
| `runtime_core` | 0 | stable | `faults`, `content_digest` |
| `atomic_fs` | 1 | stable | `faults`, `content_digest`, `identifiers`, `clock` |
| `bounded_parse` | 1 | stable | `bytes_io`, `faults` |
| `identifiers` | 1 | stable | `faults`, `clock` |
| `telemetry` | 1 | stable | `runtime_core`, `identifiers`, `faults` |
| `bio_formats` | 2 | stable | `bounded_parse`, `faults` |
| `manifests` | 2 | stable | `content_digest`, `faults`, `identifiers`, `record_io` |
| `object_store` | 2 | stable | `faults`, `content_digest`, `byte_spec`, `atomic_fs`, `clock`, `resource_version` |
| `record_io` | 2 | stable | `faults`, `content_digest`, `byte_spec` |
| `tokenizer_runtime` | 2 | stable | `faults`, `identifiers`, `content_digest` |
| `artifact_cas` | 3 | evolving | `artifact_manifest`, `object_store`, `content_digest`, `identifiers`, `faults`, `clock`, `byte_spec` |
| `checkpoint_io` | 3 | evolving | `artifact_cas`, `artifact_manifest`, `object_store`, `content_digest`, `identifiers`, `resource_version`, `clock`, `faults`, `record_io`, `byte_spec` |
| `data_stream` | 3 | evolving | `object_store`, `record_io`, `content_digest`, `identifiers`, `byte_spec`, `faults`, `retry`, `clock` |
| `gpu_host` | 3 | evolving | `faults`, `runtime_core` |
| `ipc` | 3 | evolving | `record_io`, `content_digest`, `faults`, `identifiers`, `byte_spec` |
| `servicekit` | 3 | evolving | `runtime_core`, `faults`, `identifiers`, `telemetry` |
| `telemetry_spool` | 3 | evolving | `record_io`, `atomic_fs`, `identifiers`, `clock`, `retry`, `faults`, `byte_spec` |
| `worker_protocol` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `runtime_core` |
| `python_bridge` | 4 | evolving | `artifact_cas`, `manifests`, `bytes_io`, `content_digest`, `data_stream`, `faults`, `ipc`, `tokenizer_runtime` |
| `worker_runtime` | 4 | evolving | `faults`, `runtime_core`, `worker_protocol` |
| `artifact_manifest` | 5 | compatibility | `faults`, `content_digest`, `identifiers`, `record_io` |
| `byte_spec` | 5 | compatibility | `faults` |
| `clock` | 5 | compatibility | — |
| `observability` | 5 | compatibility | `clock`, `identifiers`, `faults` |
| `python_bindings` | 5 | compatibility | `content_digest`, `tokenizer_runtime`, `artifact_cas`, `artifact_manifest`, `data_stream`, `faults`, `ipc`, `byte_spec` |
| `resource_version` | 5 | compatibility | `faults`, `content_digest` |
| `retry` | 5 | compatibility | `faults`, `clock` |
