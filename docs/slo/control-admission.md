# control.admission SLO

`control.admission` is the authoritative entitlement and budget decision boundary for one
external AI Gateway request. Production activation remains blocked until a durable repository,
atomic audit/outbox writes, reconciliation, metrics, and connected failure testing satisfy this
contract.

## Service objectives

- At least 99.95% of well-formed decisions complete within the monthly availability window;
  policy denials and budget exhaustion are successful decisions, not availability failures.
- p99 decision latency is at most 100 ms at the service boundary, excluding caller cancellation.
- No request is authorized against a stale policy epoch, an inactive policy window, an expired
  reservation, a mismatched idempotency fingerprint, or quota beyond the locked budget.
- Duplicate retries produce exactly one reservation and one terminal accounting outcome.
- Reserved capacity is reconciled to committed, released, or expired state within two reservation
  TTLs, and durable ledger/audit/outbox divergence is zero.

Correctness objectives have a zero error budget. Any overspend, stale-policy authorization,
expired replay, identity collision, or missing audit/outbox transition stops promotion and enters
the incident procedure. Availability must never be restored by bypassing admission.

Measurements use bounded decision/reason/route-class labels and never include prompts,
responses, credentials, idempotency keys, or unbounded subject/workspace identifiers. Monthly SLO
evidence, reconciliation results, and intentional-negative tests are retained with release
qualification.
