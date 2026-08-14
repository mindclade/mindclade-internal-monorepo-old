# Runbook: control-plane outage

## Expected behavior

The Go control plane is not a synchronous dependency for every online request.
During a bounded outage:

```text
already-admitted work                 continues
valid unexpired execution tickets     continue within their budgets
valid online admission grants         may admit only within local budget
new work without valid authority      is rejected
expired route/revocation snapshots    drain and fail closed
```

Durable preprocessing, ingestion, training, and batch work already leased may
continue until ticket deadline, cancellation, lease loss, or revocation.

## Immediate actions

1. Confirm database, leader election, message broker, Kubernetes API, and
   control-plane process health independently.
2. Freeze destructive administrative changes and release promotion.
3. Confirm Rust gateways have fresh route and revocation snapshots and bounded
   usage-spool capacity.
4. Confirm dispatchers/controllers have stopped claiming new work if durable
   authority cannot be verified.
5. Preserve database and outbox state; do not bypass transactions by publishing
   events manually.

## Recovery

- Restore PostgreSQL authority first, then control-plane leaders, then
  dispatchers/projectors/controllers.
- Allow inbox/outbox and projectors to replay through their normal idempotent
  paths.
- Reconcile route snapshots and minimum accepted policy/revocation epochs before
  reopening new online admission.
- Reconcile locally spooled usage and audit records before replenishing grants.
- Inspect stale leases and fencing tokens; old holders must be unable to commit.

## Exit criteria

Database consistency is confirmed, outbox backlog is draining, projectors have
caught up, route/revocation snapshots are fresh, no stale fenced worker can
commit, and new admissions are restored gradually with telemetry monitored.
