# Runbook: node preemption

## Trigger

The platform receives a preemption/termination notice or detects impending node
loss.

## Actions

1. Rust node agent/runtime host enters drain and rejects new local admissions.
2. Propagate cancellation/deadline to Python workers and external search tools.
3. Training reaches the nearest configured safe point and requests an emergency
   checkpoint within the remaining budget.
4. Work-queue handlers stop heartbeating only after outputs are committed or the
   claim is safely abandoned.
5. Flush bounded telemetry/usage/audit spools and preserve diagnostic bundle.
6. Release leases when possible; fencing protects correctness if release cannot
   complete.

## Recovery

The Go control plane requeues or reschedules unfinished work after lease/ticket
state permits it. New workers restore only from verified artifacts/checkpoints
and receive a newer fencing token.

## Exit criteria

No late process can commit, all committed artifacts verify, work is completed or
requeued once, and node-loss recovery time is recorded against the service SLO.
