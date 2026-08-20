# Deterministic Python configuration resolution

`libs.python.config` composes bounded UTF-8 TOML sources, in-memory overlays, and explicit
`path=value` overrides into an immutable `ResolvedConfig`. It records ordered source digests,
records every override, emits deterministic canonical bytes, and binds the value to a SHA-256
digest.

## Contract

Inputs are applied in caller order. Mapping values merge recursively; sequences and scalars
replace as units. Existing non-null values cannot change runtime type unless `deep_merge` is
called with `reject_type_changes=False`. Override paths use non-empty ASCII names joined by
dots, reject scalar traversal, and preserve existing types.

The returned value is a recursive read-only snapshot. Its digest is checked even when a caller
constructs `ResolvedConfig` directly, so value and identity cannot drift apart after
publication. Domain packages own semantic schemas through `RequiredField` or their own
validators; this library owns composition, structural validation, and fingerprinting.

## Bounds and failures

- source file: 8 MiB;
- source files: 64;
- in-memory overlays: 64;
- explicit overrides: 256;
- override expression: 4096 characters;
- recursive merge depth: 64.

Unreadable, invalid UTF-8, invalid TOML, unsupported JSON values, type replacement, malformed
paths, and digest mismatch fail with `InvalidArgument`. Collection or source-size exhaustion
fails with `ResourceExhausted`. Public messages do not expose filesystem paths; the bounded
path remains available as an internal diagnostic field and wrapped cause.

The resolver performs no environment-variable expansion, secret lookup, network access,
schema migration, or implicit current-directory discovery. Callers choose every source path.
