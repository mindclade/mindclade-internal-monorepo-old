# `mindclade_bounded_parse`

Safety framework for untrusted scientific inputs. Every parser gets explicit
input, line, record, token, nesting, metadata, and payload ceilings plus byte
locations and strict/recovery mode. Format semantics belong to `bio_formats`.

Every ceiling on `Limits` is inclusive — a value equal to the ceiling is
accepted, the next one is rejected — and each field documents the exact
comparison. `Recovery` is a bounded sink: it retains at most
`maximum_metadata_entries` diagnostics and counts what it dropped in
`suppressed()`, because it is the one structure here that grows per defect
rather than per input byte.
