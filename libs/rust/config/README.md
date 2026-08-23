# `mindclade_config`

Strict, provenance-carrying service configuration — the Rust counterpart of
`libs/go/config`.

Ordered sources merged with later-wins precedence, a field catalog carrying
required/default/secret/reloadable metadata, unknown-key rejection, per-value
provenance, a deterministic snapshot digest that hashes secrets rather than
carrying them, redaction through a `Secret` type with no `Debug`, `Display`, or
`Deref` to the plaintext, and atomic last-known-good reload with
restart-required reporting.

Field documentation is mandatory, so `Catalog::documentation()` always renders
the complete configuration surface. That is the usable half of the idea behind
Cloudflare's `foundations` `settings` module, taken without the dependency.

No global, no `lazy_static` that reads the environment on import, no ambient
runtime: a catalog is built explicitly and loaded by a composition root
(`libs/rust/SECURITY.md`). `EnvSource` maps canonical keys onto exact variable
names and never scans the environment, which is what keeps unknown-key
rejection meaningful.

Domain code never reads a variable directly. It receives a resolved snapshot,
or typed settings decoded from one.
