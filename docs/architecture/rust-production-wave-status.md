# Rust production-wave implementation status

| Item | Source implementation | Consumer | Promotion evidence |
|---|---|---|---|
| Cargo lock/locked metadata | qualification + lock policy | whole Rust workspace | Cargo-generated `Cargo.lock`, metadata `--locked` |
| Tokio/Tower/Axum/Bytes runtime | runtime gateway/host | online gateway + host | tests, latency/load-shed benchmarks |
| Rust promotion lane | qualification scripts + CI | all Rust crates | fmt/test/clippy/doc/fuzz/Miri/sanitizers |
| OS bulk IPC | `libs/rust/ipc_os` | runtime host | Miri/sanitizer/copy-count benchmarks |
| Ed25519 authority | `worker_protocol::signing` | gateway/host/artifact authority | Go→Rust golden signatures, rotation drills |
| Provider object store | `object_store::adapters::arrow` | artifact proxy + node agent | GCS/S3 conformance/failure injection |
| Compatibility facades | removed from workspace | none | source-manifest check |
| Panic/overflow hardening | checked arithmetic + policy checker | all hot-path crates | Clippy/tests/fuzz |
| Cross-language release blocker | golden + field-tag compatibility gate | Go/Rust/Python protocols | release gate |
| Vertical slices | integration release gate | ingestion/preprocessing/serving/training | provider/GPU-connected release evidence |

The only evidence that cannot be generated in a toolchain-less sandbox is the
Cargo-generated transitive lock and compiler/runtime qualification. The repository
fails closed on those requirements rather than fabricating evidence.
