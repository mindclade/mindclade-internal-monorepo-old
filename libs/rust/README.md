# Mindclade Shared Rust Foundation

`libs/rust` is the reusable node/runtime data-plane foundation. The uploaded Rust archive is the implementation baseline; this tree preserves its deterministic storage, artifact, checkpoint, streaming, telemetry, IPC, lifecycle, and tokenizer mechanisms and extends them with the consolidated runtime architecture.

## Production-facing crates

`runtime_core`, `bytes_io`, `content_digest`, `faults`, `identifiers`, `atomic_fs`, `record_io`, `manifests`, `object_store`, `tokenizer_runtime`, `artifact_cas`, `checkpoint_io`, `data_stream`, `ipc`, `servicekit`, `telemetry`, `telemetry_spool`, `bounded_parse`, `bio_formats`, `worker_protocol`, `worker_runtime`, `gpu_host`, and `python_bridge`.

The former `clock`, `retry`, `resource_version`, `byte_spec`, `artifact_manifest`, `observability`, and `python_bindings` crates are one-epoch compatibility surfaces and must not be used by new production code. The former broad `common` crate is removed.

Go owns durable fleet policy; Rust owns local runtime/data-plane execution; Python owns scientific/model numerics; TileLang owns qualified accelerator kernels.
