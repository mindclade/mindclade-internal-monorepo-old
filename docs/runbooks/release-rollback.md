# Runbook: release rollback

## Trigger

Numerical, safety, performance, reliability, security, or operational evidence
requires withdrawing a model, runtime, service, kernel schedule, dataset, or
configuration release.

## Actions

1. Freeze further promotion and record an immutable rollback reason/audit event.
2. Advance the minimum accepted policy/revocation epoch when online authority
   must be withdrawn immediately.
3. Publish an immutable route/deployment snapshot pointing to the last known
   good qualified bundle or enter fail-closed drain when no safe target exists.
4. Revoke affected kernel signatures so dispatch falls back to the reference
   implementation.
5. Preserve the failed release bundle, evidence, traffic split, diagnostics,
   and affected run/request IDs.

## Recovery and verification

- Confirm runtime gateways received the new route/revocation state.
- Drain old runtime hosts and prevent stale execution tickets/fencing tokens
  from committing.
- Validate the rollback target against its own signed evidence and artifact
  digests.
- Monitor error rate, latency, numerical/safety signals, capacity, and queue
  backlog during staged restoration.

## Exit criteria

All new work uses an approved target or is rejected, stale releases cannot
admit/commit, the rollback is auditable, and a corrective action prevents the
same evidence gap.
