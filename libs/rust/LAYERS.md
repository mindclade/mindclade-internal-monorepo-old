# Rust dependency layers

Production dependencies are downward-only.

- Layer 0: `faults`, `content_digest`, `runtime_core`, `bytes_io`
- Layer 1: `identifiers`, `atomic_fs`, `telemetry`, `bounded_parse`
- Layer 2: `record_io`, `manifests`, `object_store`, `tokenizer_runtime`, `bio_formats`
- Layer 3: `artifact_cas`, `checkpoint_io`, `data_stream`, `ipc`, `servicekit`, `telemetry_spool`, `worker_protocol`, `gpu_host`
- Layer 4: `worker_runtime`, `python_bridge`
- Layer 5: compatibility-only crates; forbidden to new production consumers

`libs/rust` may not depend on control, models, training, serving policy, product APIs, or executable services. Provider adapters live at service leaves.
