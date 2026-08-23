# Rust runtime foundation guide

The user-supplied Rust library is the starting implementation. The optimized
layout preserves its bounded/deterministic mechanisms while consolidating
execution concerns into cohesive production crates.

## Canonical crates

- `runtime_core`: clocks/deadlines, cancellation, retry, content-bound resource
  versions, fencing, hierarchical resource budgets, task groups.
- `bytes_io`: checked byte sizes/ranges/alignment, buffer pools and copy
  accounting.
- `content_digest`: stable digest API over pinned RustCrypto SHA-256 and streaming
  verification.
- `bounded_parse`: parser budgets, strict/recovery modes and byte diagnostics.
- `bio_formats`: bounded scientific format framing/parsing.
- `manifests`: immutable artifact identity/location and manifest primitives.
- `object_store`: provider-neutral conditional/range storage mechanisms.
- `artifact_cas`: content-addressed storage/retention/GC mechanisms.
- `checkpoint_io`: transactional shard/session publication and verification.
- `data_stream`: deterministic bounded/resumable streaming.
- `ipc`: integrity-checked bounded control frames.
- `worker_protocol`: signed authority, route/revocation state, worker commands
  and bulk buffer descriptors.
- `worker_runtime`: explicit ticketed worker state machine.
- `gpu_host`: provider-neutral GPU/model-slot budget reservation.
- `servicekit`: process lifecycle and supervised tasks.
- `telemetry` / `telemetry_spool`: bounded structured telemetry and durable
  delivery.
- `python_bridge`: leaf in-process bridge primitives; long-lived workers stay
  process isolated.

## Compatibility epoch (closed)

The uploaded library exposed earlier crates such as `clock`, `retry`,
`resource_version`, `byte_spec`, `artifact_manifest`, `observability`, and
`python_bindings`. The migration epoch is **over**: all seven were removed
outright, not kept as facades. They are gone from `libs/rust/`, from the
workspace members, and from `Cargo.lock`, so a stale import of one is a compile
error rather than a deprecation warning.

`BASELINE` in `check_rust_workspace.py` is now empty — there are no legacy
compatibility edges left to record, so *every* edge to a retired crate is
rejected, not just new ones.
`tools/analysis/check_code_docs_alignment.py` additionally fails if any
`libs/rust/<name>` directory reappears or if any file under `libs/rust` names one
of the retired crates. All source must use the canonical crates above; see
`docs/migrations/rust-compatibility-crates.md` for the name-by-name mapping.

## Safety/dependencies

Foundation/runtime crates default to `#![forbid(unsafe_code)]`. An OS/GPU/Python
adapter that genuinely requires unsafe Rust must be isolated, have `SAFETY.md`,
owner approval, an unsafe inventory/invariants, and Miri/sanitizer/fuzz
qualification. Curated dependencies are exactly pinned in the committed Cargo
graph and checked for Cargo/Bazel alignment, licenses, advisories, and admitted
sources. Zero third-party dependencies is not a goal when it would reimplement
mature cryptography, async runtime, or provider infrastructure.

## Qualification boundary

The 2026-08-20 canonical Rust presubmit passes with the pinned Rust 1.97.1
toolchain: locked tests, formatting, strict Clippy, rustdoc, cargo-deny,
compatibility, six local failure scenarios, and portable digest, reservation,
Unix IPC, process-start, verified-range, contention, RSS, and FD probes.
Production promotion still requires connected Linux unsafe-code evidence,
fuzzing, Miri/sanitizers, Bazel/Nix release hermeticity, provider qualification,
deployed gateway and hardware/provider performance budgets, and deployment evidence.
