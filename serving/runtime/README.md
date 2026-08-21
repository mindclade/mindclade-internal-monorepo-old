# Serving / Runtime

**Language:** Rust
**Status:** implemented reusable online-serving core; deployment qualification pending.

This crate owns local ticket/grant validation, admission reservations, bounded streaming,
coarse batching envelopes, route/policy snapshot behavior, health, telemetry, and deterministic
control-plane-outage semantics used by the runtime gateway and host. It is provider-neutral and
contains no Python numerical engine or durable control-plane state.

All streams, reservations, route tables, snapshots, diagnostics, and retry paths have explicit
ceilings. New admission fails closed when authority is absent, expired, revoked, or saturated;
already admitted work may continue only inside its valid signed budget. Cancellation and terminal
stream states are explicit, and the crate forbids unsafe code.

The runtime core being implemented does not make a deployment production-ready. Gateway/host
composition still requires digest-pinned images, connected ticket/signature and revocation tests,
network and failure injection, accelerator/model parity, latency/capacity evidence, security
review, GitOps canary/rollback evidence, and linked SLO/runbook qualification.
