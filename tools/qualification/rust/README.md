# Rust qualification

This directory is the executable promotion gate for the Rust online/runtime data plane.
It is no longer a scaffold.

The pinned production compiler is Rust **1.97.1**. The presubmit lane requires a
Cargo-generated lockfile, `cargo metadata --locked`, `cargo fmt --check`, workspace
unit/integration/doc tests, and Clippy with warnings denied. Nightly/release add
fuzzing and Miri; sanitizer jobs run on the pinned nightly companion toolchain for
OS/runtime leaf crates.

Foundation crates remain dependency-light. Tokio/Tower/Axum/Tonic/Bytes,
`object_store`, Ed25519, libc, and provider dependencies are restricted to runtime,
protocol, cryptographic, cloud, and OS adapters.

Release mode also collects fresh production evidence. It requires the gateway
resolver URL, a signed request fixture, the gateway PID, a dedicated cancellation
probe command, an exact GKE context, a digest-pinned qualification image, and a
bounded run ID:

```bash
MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_URL=http://gateway/v1/runtime/resolve \
MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_REQUEST=/absolute/request.pb \
MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_PID="$GATEWAY_PID" \
MINDCLADE_RUNTIME_GATEWAY_CANCELLATION_COMMAND=/absolute/cancellation-probe \
MINDCLADE_GKE_CLUSTER_CONTEXT="$GKE_CONTEXT" \
MINDCLADE_QUALIFICATION_IMAGE='registry/image@sha256:…' \
MINDCLADE_QUALIFICATION_RUN_ID="$RUN_ID" \
python tools/qualification/rust/qualify.py --mode release
```

The release gate submits both repository-owned H100 and H200 Jobs and rejects
missing credentials, capacity, quota, image digests, profiles, or metrics. It does
not accept pre-authored hardware evidence. See
`tools/qualification/gke/README.md` for the cluster and image contract.
