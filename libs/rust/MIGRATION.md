# Rust Foundation Migration

The former placeholder modules are replaced by narrow crates. New code must not
add a catch-all utility crate.

| Former placeholder | Authoritative replacement |
|---|---|
| `common/error` | `faults` |
| `common/ids` | `identifiers` |
| `common/time` | `runtime_core` |
| ad-hoc hashes | `content_digest` |
| generic file helpers | `atomic_fs` |
| generic storage helpers | `object_store` |
| checkpoint helpers | `checkpoint_io` |
| dataset readers | `data_stream` |
| telemetry journal | `telemetry_spool` |

`common` is gone. It is not a facade, a re-export shim, or a namespace: the
directory does not exist and `tools/analysis/check_rust_workspace.py` fails the
build if `libs/rust/common` reappears. The 2026-08 epoch closed the compatibility
window for every narrow facade as well — see `MIGRATION_2026_08.md`.
