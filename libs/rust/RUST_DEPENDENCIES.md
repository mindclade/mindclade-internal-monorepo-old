# Rust Dependency Inventory

The foundation minimizes rather than prohibits third-party dependencies. Its
direct external crates are `bytes`, `ed25519-dalek`, `libc`, `object_store`,
`seccompiler`, `sha2`, and `tokio`; their exact versions and transitive closure
are recorded in the root `Cargo.toml` and Cargo-generated `Cargo.lock`. Git
dependencies are not admitted.

`seccompiler` is a Linux-only target dependency of `sandbox_os` and adds nothing
to the closure: `libc` is its single non-optional dependency and was already
pinned. It was chosen over `libseccomp` bindings and Cloudflare's `foundations`
because it needs no C library, no build script, and no `bindgen`/`libclang`, and
because keeping the `seccomp(2)`/`prctl(2)` calls behind its safe API is what
lets `sandbox_os` stay `unsafe_code = "deny"` instead of becoming a third
exception under `UNSAFE_POLICY.md`. See ADR-0027.

Every external dependency is subject to Cargo/Bazel alignment, checksum,
license, advisory, and source policy. Provider SDKs and runtime frameworks must
not leak into domain-neutral contracts without a dependency-policy review.
