# Rust runtime foundation guide

The user-supplied Rust library is the starting implementation. The optimized
layout preserves its bounded/deterministic mechanisms while consolidating
execution concerns into cohesive production crates.

## Canonical crates

- `runtime_core`: clocks/deadlines, cancellation, retry, content-bound resource
  versions, fencing, hierarchical resource budgets, task groups.
- `bytes_io`: checked byte sizes/ranges/alignment, buffer pools and copy
  accounting.
- `content_digest`: dependency-free SHA-256 and streaming verification.
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

## Compatibility epoch

The uploaded library exposed earlier crates such as `clock`, `retry`,
`resource_version`, `byte_spec`, `artifact_manifest`, `observability`, and
`python_bindings`. They remain for one migration epoch so existing code is not
needlessly broken. `check_rust_workspace.py` records the exact legacy
compatibility edges and rejects **new** consumers. New source must use the
canonical crates above.

## Safety/dependencies

Foundation/runtime crates default to `#![forbid(unsafe_code)]`. An OS/GPU/Python
adapter that genuinely requires unsafe Rust must be isolated, have `SAFETY.md`,
owner approval, an unsafe inventory/invariants, and Miri/sanitizer/fuzz
qualification. Curated runtime dependencies (Tokio/Tower/Tonic/Bytes/etc.) may
be introduced in leaf/runtime adapters when the pinned build graph is available;
zero third-party dependencies is not a goal if it would cause reimplementation
of mature infrastructure.

## Qualification still required

This source snapshot was produced in an environment without `rustc`, Cargo,
Bazel or Nix. Rust status is therefore **implemented source, not compiled or
qualified** here. Connected CI must run formatting, compile, Clippy, tests,
fuzz targets, Miri/sanitizers where applicable, Bazel/Nix hermeticity, and the
runtime latency/memory/cancellation/shutdown budgets before promotion.
