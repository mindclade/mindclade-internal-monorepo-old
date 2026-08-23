# Shared Rust Package Catalog

Generated from the Cargo manifests by
`tools/analysis/check_rust_package_manifest.py --write`, which also gates it. Edit the
crate `Cargo.toml` files, `layers.json`, or `stability.json` — not this table.

Production crates: **26**  
Compatibility crates: **0**

| Package | Layer | Status | Direct internal dependencies |
|---|---:|---|---|
| `bytes_io` | 0 | stable | `faults` |
| `content_digest` | 0 | stable | `faults` |
| `faults` | 0 | stable | — |
| `process_os` | 0 | evolving | `faults` |
| `runtime_core` | 0 | stable | `content_digest`, `faults` |
| `sandbox_os` | 0 | evolving | `faults` |
| `atomic_fs` | 1 | stable | `content_digest`, `faults`, `identifiers`, `runtime_core` |
| `bounded_parse` | 1 | stable | `bytes_io`, `faults` |
| `config` | 1 | evolving | `content_digest`, `faults` |
| `identifiers` | 1 | stable | `content_digest`, `faults`, `runtime_core` |
| `telemetry` | 1 | stable | `faults`, `identifiers`, `runtime_core` |
| `bio_formats` | 2 | stable | `bounded_parse`, `faults` |
| `manifests` | 2 | stable | `content_digest`, `faults`, `identifiers`, `record_io` |
| `object_store` | 2 | stable | `atomic_fs`, `bytes_io`, `content_digest`, `faults`, `runtime_core` |
| `record_io` | 2 | stable | `bytes_io`, `content_digest`, `faults` |
| `tokenizer_runtime` | 2 | stable | `content_digest`, `faults`, `identifiers` |
| `artifact_cas` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `identifiers`, `manifests`, `object_store`, `runtime_core` |
| `checkpoint_io` | 3 | evolving | `artifact_cas`, `bytes_io`, `content_digest`, `faults`, `identifiers`, `manifests`, `object_store`, `record_io`, `runtime_core` |
| `data_stream` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `identifiers`, `object_store`, `record_io`, `runtime_core` |
| `gpu_host` | 3 | evolving | `content_digest`, `faults`, `runtime_core` |
| `ipc` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `identifiers`, `record_io`, `worker_protocol` |
| `ipc_os` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `worker_protocol` |
| `servicekit` | 3 | evolving | `faults`, `identifiers`, `runtime_core`, `telemetry` |
| `telemetry_spool` | 3 | evolving | `atomic_fs`, `bytes_io`, `faults`, `identifiers`, `record_io`, `runtime_core`, `telemetry` |
| `worker_protocol` | 3 | evolving | `bytes_io`, `content_digest`, `faults`, `identifiers`, `runtime_core` |
| `python_bridge` | 4 | evolving | `artifact_cas`, `bio_formats`, `bounded_parse`, `bytes_io`, `content_digest`, `data_stream`, `faults`, `ipc`, `manifests`, `tokenizer_runtime`, `worker_protocol` |
| `worker_runtime` | 4 | evolving | `faults`, `runtime_core`, `sandbox_os`, `worker_protocol` |

Layer 5 is the compatibility tier. It is empty: the 2026-08 epoch crates were removed,
not deprecated. See `MIGRATION_2026_08.md` for the replacement mechanism of each one.
