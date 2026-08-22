# control.admission runbook

## Trigger

Use this runbook for admission-store unavailability, unexpected budget exhaustion, ledger drift,
stuck or expired reservations, stale-policy decisions, idempotency conflicts, or an observed
authorization/accounting invariant violation.

## control-admission-api-metric-contract-incomplete

Compare the desired API replica count with the versioned per-replica inventory: 60 decision
counter series, 72 histogram buckets, six histogram counts, and six histogram sums. Treat a
missing series as missing SLI evidence; do not reconstruct traffic from logs or another replica.

## control-admission-api-target-absent

Confirm GMP target freshness, the named `metrics` port, and the environment-owned collector
NetworkPolicy identity. Keep the Gateway fail closed; do not infer API health from application
logs or another process role.

## control-admission-audit-outbox-drift

Freeze affected admission routes, preserve bounded reconciliation evidence, and classify the
fixed drift kind before replaying canonical transaction evidence. Never synthesize an audit or
outbox success from downstream state.

## control-admission-backlog-after-two-successful-sweeps

Inspect the maintenance lease, completed workqueue outcomes, bounded batch saturation, and
database lock contention. Restore the leased worker path; do not start an unleased ad-hoc writer.

## control-admission-decision-latency-slo-breached

Confirm the decision-volume sample guard and the exact 100-millisecond compliance ratio, then use
diagnostic p99 split by the fixed operation allowlist to inspect database latency, lock wait, and
dependency saturation. Do not bypass admission to recover latency.

## control-admission-expiration-snapshot-stale

Inspect bounded-query timeout and snapshot-success telemetry. Treat the last cached backlog value
as unknown until a new successful expiration sample is recorded.

## control-admission-expired-reservation-age

Keep expired reservations invalid for commit and replay, even before durable state materializes.
Inspect the maintenance lease and bounded sweeper, then prove age returns below 15 seconds.

## control-admission-fast-error-budget-burn

Verify that both the five-minute and one-hour windows breach and that only `unavailable`,
`internal`, and server-owned `deadline` results consume availability budget. Caller cancellation
is excluded. Contain the failing dependency or process without reclassifying policy denial or
bypassing admission.

## control-admission-lineage-snapshot-stale

Treat drift as unknown, preserve the reconciliation timeout evidence, and restore the bounded
indexed sampler before relying on a cached zero.

## control-admission-maintenance-metric-contract-incomplete

Compare every desired maintenance replica with the versioned four-scalar, three-drift-kind,
two-snapshot-timestamp, and two-snapshot-outcome inventory. Keep backlog and reconciliation state
unknown until every replica exports the complete fixed contract.

## control-admission-maintenance-target-absent

Confirm maintenance Pod health, scrape target freshness, and exact collector ingress. Keep sweep
and reconciliation health unknown until both target and fresh snapshot evidence return.

## control-admission-slow-error-budget-burn

Verify that both the thirty-minute and six-hour windows breach, review the failure distribution and
recent releases, then plan a controlled rollback or dependency repair. Do not wait for the monthly
objective to be exhausted.

## control-admission-sweep-stale

Inspect the leader lease and latest completed expiration work item. Keep the route fail closed if
expired state cannot be materialized within the objective, and never mutate reservation rows
directly.

## Containment and diagnosis

1. Put the affected Gateway route into fail-closed or drain mode. Do not bypass admission, widen a
   budget, reuse a policy version, or directly mutate durable reservation rows.
2. Preserve request digests, canonical reservation/policy IDs and versions, audit records, outbox
   positions, database transaction evidence, and bounded telemetry. Never collect provider
   credentials or request/response payloads.
3. Determine whether the fault is repository availability, stale entitlement/budget state,
   idempotency-index divergence, quota-ledger drift, expiration backlog, or audit/outbox lag.
   Compare bounded backlog size, oldest expired-but-reserved age, last successful sweep time, and
   reconciliation drift. A reservation past `expires_at` is never valid authorization even if its
   durable state has not yet been materialized as `expired`.
4. For possible overspend or stale authorization, freeze new reservations for the affected budget
   and escalate to platform-control and security incident command.

## Recovery

1. Reconcile reservations from the authoritative durable transaction log. Terminal transitions
   are forward-only; do not turn committed, released, or expired records back into reservations.
2. Repair missing audit/outbox projections by replaying the canonical transaction evidence. Never
   synthesize a successful authorization from downstream provider or MLflow state.
3. Restore the leader-gated bounded expiration sweeper; do not run an unleased ad-hoc writer.
   Prove that the backlog reaches zero, the oldest-expired age returns below the 15-second
   objective, and expired capacity and idempotency records agree before reopening the route.
4. Apply schema or policy corrections through reviewed migrations with a new monotonic resource
   version. Roll forward after a partial migration; never roll durable state backward.

## Exit criteria

Re-enable traffic only after concurrent reservation tests, stale-policy and expired-replay negative
tests, ledger reconciliation, audit/outbox parity, availability/latency checks, and post-recovery
drift all pass. Retain the exact source revision, data-range bounds, commands, results, and incident
follow-up with release evidence.
