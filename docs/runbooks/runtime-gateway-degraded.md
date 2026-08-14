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
