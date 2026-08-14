# Removed Rust compatibility crates

**Status:** Removed after the 2026-08 runtime consolidation.

The following crate names were temporary migration facades and are no longer workspace members or source packages:

- `mindclade_clock` → `mindclade_runtime_core::{Clock, SystemClock, ManualClock}`
- `mindclade_retry` → `mindclade_runtime_core::{Policy, execute}`
- `mindclade_resource_version` → `mindclade_runtime_core::ResourceVersion`
- `mindclade_observability` → `mindclade_telemetry`
- `mindclade_artifact_manifest` → `mindclade_manifests`
- `mindclade_byte_spec` → `mindclade_bytes_io`
- `mindclade_python_bindings` → `mindclade_python_bridge`

Production code must use the canonical crates directly. Reintroducing an alias crate requires an ADR and a time-bounded removal plan.
