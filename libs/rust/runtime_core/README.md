# `mindclade_runtime_core`

Canonical execution-only primitives for node/runtime code: clocks, monotonic
Deadlines, deterministic retries, content-bound resource versions, cancellation,
fencing, hierarchical resource budgets, and owned task groups.

It may not depend on storage, protocols, parsers, Python, CUDA, cloud SDKs, or
service-specific policy. The legacy `clock`, `retry`, and `resource_version`
crates are compatibility surfaces for one migration epoch; new production code
uses this crate.
