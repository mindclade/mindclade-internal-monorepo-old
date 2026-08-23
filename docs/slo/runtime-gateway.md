# Runtime gateway SLO

**Status:** no approved objective. The service cannot currently report itself ready.

Objectives are defined before production promotion. `services/runtime_gateway` is not at that point,
and the blocker is not a missing measurement but a defect in the readiness signal itself — see
below. A component that cannot advertise readiness cannot be placed behind an availability target,
because the target would be measured against traffic an orchestrator should never have sent.

## Blocking defect — `/readyz` never returns 200 in production

`GatewayHealthSnapshot::ready()` requires all three flags:

```rust
self.accepting && self.policy_fresh && self.runtime_host_ready
```

(`services/runtime_gateway/src/health.rs:22-24`). All three initialise to `false` (`:39-41`). The
production start hook sets exactly two of them — `set_policy_fresh(true)` and
`core.resume_admission()` (`services/runtime_gateway/src/lifecycle.rs:41-45`).
`set_runtime_host_ready` is defined at `services/runtime_gateway/src/health.rs:50` and is called
from exactly two places, **both of them tests**
(`services/runtime_gateway/tests/integration.rs:147`,
`services/runtime_gateway/tests/shutdown.rs:13`).

`runtime_host` does the equivalent correctly, asserting its readiness sub-flags during construction
(`services/runtime_host/src/server.rs:81-82`), which confirms the gateway behaviour is a defect and
not a deliberate design. PR #113 reported this and did not fix it. Recovery guidance is in
`docs/runbooks/runtime-gateway-degraded.md`.

Until this is fixed, an availability objective for this service would be measuring a path that
readiness gating is supposed to keep closed.

## Unratified candidate — not an agreed target

A previous revision recorded `99.9%` availability "for admitted production traffic where
applicable", identically to four other unrelated SLO documents and with no owner, window, or
measurement. It is retained here as an **unratified candidate**, not an agreed commitment, so the
earlier choice remains on the record while carrying no authority.

## No instrumentation exists

This service emits no metrics, no structured logs, and no traces. Every indicator for it must come
from the caller or from the surrounding infrastructure. `execution_enabled` also defaults to
`false` (`services/runtime_gateway/src/config.rs:82`), so a default-configured gateway does not
execute at all.

## Bounds already enforced

These are real and can be asserted today. They constrain any future objective; they are not
themselves objectives.

| Bound | Value | Source |
| --- | --- | --- |
| `MAX_DISPATCH_BYTES` | 1 MiB | `src/network.rs:34` |
| `MAX_NETWORK_CONCURRENCY` | 8192 | `src/network.rs:35` |
| `GATEWAY_DRAIN_TIMEOUT` | 30 s | `src/network.rs:36` |
| `maximum_active_requests` | 4096 | `src/config.rs:72` |
| `maximum_active_requests_per_grant` | 128 | `src/config.rs:73` |
| `maximum_stream_chunks` | 256 | `src/config.rs:74` |
| `maximum_stream_chunk_bytes` | 1 MiB | `src/config.rs:75` |
| `maximum_stream_output_bytes` | 64 MiB | `src/config.rs:76` |
| `execution_enabled` | `false` | `src/config.rs:82` |

## Correctness invariants (release-blocking, not traded for availability)

No request is admitted outside valid ticket authority, and bounded queues are never converted into
unbounded buffers to absorb load. Bounded admission, cancellation, and shutdown budgets must be
release-qualified before production promotion; they are not release-qualified today. SLO exclusions
require an incident or evidence record, not an ad hoc dashboard annotation.
