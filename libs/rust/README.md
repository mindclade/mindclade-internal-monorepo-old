# Mindclade Shared Rust Foundation

`libs/rust` is the reusable node/runtime data-plane foundation. The uploaded Rust archive is the implementation baseline; this tree preserves its deterministic storage, artifact, checkpoint, streaming, telemetry, IPC, lifecycle, and tokenizer mechanisms and extends them with the consolidated runtime architecture.

## Production-facing crates

`runtime_core`, `bytes_io`, `content_digest`, `faults`, `identifiers`, `atomic_fs`, `process_os`, `record_io`, `manifests`, `object_store`, `tokenizer_runtime`, `artifact_cas`, `checkpoint_io`, `data_stream`, `ipc`, `ipc_os`, `servicekit`, `telemetry`, `telemetry_spool`, `bounded_parse`, `bio_formats`, `worker_protocol`, `worker_runtime`, `gpu_host`, and `python_bridge`.

`PACKAGE_CATALOG.md` carries the same inventory with each crate's layer, stability, and exact
internal dependency edges, derived from the Cargo manifests.

The former `clock`, `retry`, `resource_version`, `byte_spec`, `artifact_manifest`,
`observability`, and `python_bindings` crates are **removed**, not deprecated: their
directories, workspace members, and `Cargo.lock` entries are gone, and their mechanisms live in
`runtime_core`, `bytes_io`, `manifests`, `telemetry`, and `python_bridge`. There is no
compatibility facade left to import. The former broad `common` crate is removed, and a
catch-all crate of that shape is forbidden rather than merely discouraged.

Go owns durable fleet policy; Rust owns local runtime/data-plane execution; Python owns scientific/model numerics; TileLang owns qualified accelerator kernels.
