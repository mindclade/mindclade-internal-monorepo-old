# Rust Foundation Migration

The former placeholder modules are replaced by narrow crates. New code must not
add a catch-all utility crate.

| Former placeholder | Authoritative replacement |
|---|---|
| `common/error` | `faults` |
| `common/ids` | `identifiers` |
| `common/time` | `clock` |
| ad-hoc hashes | `content_digest` |
| generic file helpers | `atomic_fs` |
| generic storage helpers | `object_store` |
| checkpoint helpers | `checkpoint_io` |
| dataset readers | `data_stream` |
| telemetry journal | `telemetry_spool` |

`common` remains a named-submodule compatibility facade for one internal API
epoch and contains no implementation.
