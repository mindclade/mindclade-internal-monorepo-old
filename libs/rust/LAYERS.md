# Rust dependency layers

Production dependencies never point upward: a crate at layer *n* may depend only on crates at
layer *n* or below. `layers.json` is the machine-readable assignment and
`tools/analysis/check_rust_package_manifest.py` gates both the assignment and the direction
against the Cargo manifests, so this list and that file cannot drift apart.

- Layer 0: `faults`, `content_digest`, `runtime_core`, `bytes_io`, `process_os`, `sandbox_os`
- Layer 1: `identifiers`, `atomic_fs`, `telemetry`, `bounded_parse`
- Layer 2: `record_io`, `manifests`, `object_store`, `tokenizer_runtime`, `bio_formats`
- Layer 3: `artifact_cas`, `checkpoint_io`, `data_stream`, `ipc`, `ipc_os`, `servicekit`, `telemetry_spool`, `worker_protocol`, `gpu_host`
- Layer 4: `worker_runtime`, `python_bridge`
- Layer 5: compatibility tier — empty

Same-layer edges are permitted and several exist today (`runtime_core` on `content_digest`,
`atomic_fs` on `identifiers`, `manifests` on `record_io`, `checkpoint_io` on `artifact_cas`,
`ipc` and `ipc_os` on `worker_protocol`). They stay acyclic because Cargo rejects a dependency
cycle outright. A layer number is therefore a ceiling on what a crate may reach, not a promise
that its peers are unreachable; read the catalog when you need the exact edge set.

Layer 5 held the 2026-08 compatibility facades. Those crates — `clock`, `retry`,
`resource_version`, `observability`, `artifact_manifest`, `byte_spec`, and `python_bindings` —
were **removed**, not deprecated: the directories, the workspace members, and the `Cargo.lock`
entries are all gone, and their mechanisms live in `runtime_core`, `telemetry`, `manifests`,
`bytes_io`, and `python_bridge`. The tier is kept as a named concept for the next compatibility
epoch, not as a place anything currently sits. See `MIGRATION_2026_08.md`.

`libs/rust` may not depend on control, models, training, serving policy, product APIs, or executable services. Provider adapters live at service leaves.
