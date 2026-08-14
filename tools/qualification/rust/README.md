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
