# control.admission runbook

## Trigger

Use this runbook for admission-store unavailability, unexpected budget exhaustion, ledger drift,
stuck or expired reservations, stale-policy decisions, idempotency conflicts, or an observed
authorization/accounting invariant violation.

## Containment and diagnosis

1. Put the affected Gateway route into fail-closed or drain mode. Do not bypass admission, widen a
   budget, reuse a policy version, or directly mutate durable reservation rows.
2. Preserve request digests, canonical reservation/policy IDs and versions, audit records, outbox
   positions, database transaction evidence, and bounded telemetry. Never collect provider
   credentials or request/response payloads.
3. Determine whether the fault is repository availability, stale entitlement/budget state,
   idempotency-index divergence, quota-ledger drift, expiration backlog, or audit/outbox lag.
4. For possible overspend or stale authorization, freeze new reservations for the affected budget
   and escalate to platform-control and security incident command.

## Recovery

1. Reconcile reservations from the authoritative durable transaction log. Terminal transitions
   are forward-only; do not turn committed, released, or expired records back into reservations.
2. Repair missing audit/outbox projections by replaying the canonical transaction evidence. Never
   synthesize a successful authorization from downstream provider or MLflow state.
3. Run the bounded expiration sweeper and prove that expired capacity and idempotency records agree
   before reopening the route.
4. Apply schema or policy corrections through reviewed migrations with a new monotonic resource
   version. Roll forward after a partial migration; never roll durable state backward.

## Exit criteria

Re-enable traffic only after concurrent reservation tests, stale-policy and expired-replay negative
tests, ledger reconciliation, audit/outbox parity, availability/latency checks, and post-recovery
drift all pass. Retain the exact source revision, data-range bounds, commands, results, and incident
follow-up with release evidence.
