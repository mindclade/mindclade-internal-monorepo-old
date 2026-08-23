# Runbook: runtime gateway degraded

## Trigger

Elevated p95/p99 latency, admission failures, streaming interruptions, queue
saturation, usage-spool pressure, stale route snapshots, ticket verification
failures, or repeated runtime-host errors.

## Triage

1. Break latency into authentication/ticket validation, route lookup, local
   admission, host IPC, Python queue/model execution, and response streaming.
2. Inspect bounded queue depth, active grants, local resource budget,
   connections, file descriptors, task count, cache state, snapshot age, and
   host health.
3. Verify policy/revocation epoch and signing key set freshness.

## Recovery

- Saturation: load shed or reduce admission; never convert bounded queues into
  unbounded buffers.
- Stale/expired policy state: reject new work and drain according to policy.
- Runtime host/model worker fault: remove the host from local routing and
  restart through supervised lifecycle.
- Ticket/key failure: follow `ticket-key-rotation.md` when rotation-related;
  otherwise reject rather than bypass verification.
- Control-plane outage: follow `control-plane-outage.md`.

## Exit criteria

Snapshots and keys are fresh, queues/resources are within bounds, cancellation
and streaming work, latency/error SLOs recover, and no request was admitted
outside valid authority.

## Known limitation: `/readyz` never returns 200 in production

Before triaging a readiness alert, know that this service **cannot currently report itself ready**,
and that this is a defect rather than a symptom of the incident being investigated.

`GatewayHealthSnapshot::ready()` requires all three flags — `accepting && policy_fresh &&
runtime_host_ready` (`services/runtime_gateway/src/health.rs:23-25`) — and all three initialise to
`false` (`:39-41`). The production start hook sets exactly two of them,
`set_policy_fresh(true)` and `core.resume_admission()`
(`services/runtime_gateway/src/lifecycle.rs:44-48`). `set_runtime_host_ready`
(`services/runtime_gateway/src/health.rs:50`) is called from exactly three places, and **all are
tests** (`services/runtime_gateway/tests/integration.rs:147`,
`services/runtime_gateway/tests/shutdown.rs:41,82`).

`runtime_host` asserts its equivalent readiness sub-flags during construction
(`services/runtime_host/src/server.rs:81-82`), which is what makes this a gateway defect rather
than an intended design. PR #113 reported it and did not fix it.

Operational consequences:

- A failing `/readyz` on this service is expected and carries **no diagnostic information**. Do not
  spend triage time on it, and do not treat a recovery as confirmed because readiness returned —
  it will not.
- Readiness-gated rollout, load-balancer membership, and any orchestration that waits for ready
  will never proceed for this service. If it is receiving traffic, something is bypassing readiness
  gating; establish what, because that path is unprotected during startup and drain.
- Liveness is unaffected: `live()` returns `true` unconditionally
  (`services/runtime_gateway/src/health.rs:19-21`).
- Until this is fixed, use the drain and admission state directly rather than the readiness
  endpoint when deciding whether the gateway is serving.

The fix is to call `set_runtime_host_ready` from the production lifecycle once host connectivity is
established, mirroring `runtime_host`. Do **not** work around it by removing `runtime_host_ready`
from `ready()`; that would make readiness pass while the host link is genuinely unverified, which
is worse than the current failure.
