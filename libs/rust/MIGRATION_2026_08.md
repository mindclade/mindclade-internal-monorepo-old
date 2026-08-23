# Rust runtime consolidation epoch — 2026-08

The uploaded 21-crate foundation is the source baseline. Stable implementations
are migrated, not rewritten: `clock`/`retry`/`resource_version` -> `runtime_core`,
`byte_spec` -> `bytes_io`, `artifact_manifest` -> `manifests`, `observability` ->
`telemetry`, and `python_bindings` -> `python_bridge`. The broad `common`
compatibility crate is removed immediately.

**The compatibility epoch is closed.** The seven narrow crates above were kept for
one epoch and have since been removed outright: their directories, workspace
members, and `Cargo.lock` entries are gone. Nothing re-exports them, so a stale
import of a retired crate is a compile error rather than a deprecation warning.
`tools/analysis/check_code_docs_alignment.py` fails if any of those directories
reappears or if any file under `libs/rust` names one of the retired crates, and
`tools/analysis/check_rust_package_manifest.py` keeps them out of the declared
package inventory.

New production crates add `bounded_parse`, `bio_formats`, `worker_protocol`,
`worker_runtime`, and `gpu_host`; the OS adapters `ipc_os` and `process_os`
followed. This keeps mechanisms cohesive without splitting every operation into
its own crate.
