# Rust Dependency Inventory

The foundation minimizes rather than prohibits third-party dependencies. Its
direct external crates are `bytes`, `ed25519-dalek`, `libc`, `object_store`,
`sha2`, and `tokio`; their exact versions and transitive closure are recorded in
the root `Cargo.toml` and Cargo-generated `Cargo.lock`. Git dependencies are not
admitted.

Every external dependency is subject to Cargo/Bazel alignment, checksum,
license, advisory, and source policy. Provider SDKs and runtime frameworks must
not leak into domain-neutral contracts without a dependency-policy review.
