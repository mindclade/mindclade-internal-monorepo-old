# Rust production hardening

This chapter records the post-18-optimization production wave. It does not change
language authority: Go owns durable fleet policy, Rust owns the online/node data
plane, Python owns scientific and tensor semantics, and TileLang owns qualified GPU
kernels.

## Runtime dependency classes

Foundation crates stay dependency-light. External dependencies are restricted to
leaf/runtime responsibilities:

- Tokio, Axum, Tower, Bytes: runtime network/process edges;
- Tonic/Prost: versioned cross-language control protocols;
- `object_store`: cloud-provider mechanics below Mindclade namespace/digest policy;
- Ed25519: offline verification of Go-issued signed authority;
- libc: audited Linux memfd/file-descriptor IPC only.

Every dependency requires a Cargo-generated lock, Bazel/rules_rust lock parity, Nix
supply-chain pinning, SBOM inclusion, vulnerability scanning, and ownership review.

## Bulk IPC

Large tensors, feature bundles, MSA matrices and checkpoints do not transit gRPC.
`ipc_os` implements Linux memfd segments created CLOEXEC and permanently sealed
against write, growth, and truncation after initialization, with explicit
generation, byte range, content digest, owner, lease expiration and access
mode. Registry capacity is reserved without holding its mutex across OS I/O.
The control protocol carries only `BufferDescriptor` values. The portable file
fallback publishes create-new files atomically with owner-only Unix permissions.
Non-Linux platforms must qualify that fallback and never silently copy
unbounded payloads.

## Signing

Go remains the issuance authority. Rust `worker_protocol::signing` validates Ed25519
signatures from an immutable bounded keyset with key IDs, not-before/not-after
windows and emergency disable state. Runtime services validate locally; no admitted
request synchronously depends on Go. Production KMS private keys never enter Rust.

## Provider object storage

`object_store::adapters::arrow` wraps Apache `object_store`. Provider mechanics are
not policy: the Mindclade wrapper still enforces namespace boundaries, request byte
limits, deadlines, content-addressed identity and digest verification. Artifact proxy
and node agent are direct consumers.

## Promotion

`tools/qualification/rust/qualify.py` is the canonical Cargo qualification lane.
Presubmit requires the pinned Rust 1.97.1 compiler, generated lockfile, rustfmt,
workspace tests, doc tests and Clippy with warnings denied. Nightly/release add Miri,
fuzzing and sanitizer/failure-injection lanes. Release also runs cross-language wire
compatibility and the four architecture-defining vertical slices.

The workspace deliberately exempts `clippy::missing_errors_doc`: public
fallible APIs use the shared typed `Fault` taxonomy documented at module and
contract level. All other `all` and `pedantic` findings remain warnings and are
promoted to errors in qualification; narrow algorithm/generated-code exceptions
carry source-local rationale.
