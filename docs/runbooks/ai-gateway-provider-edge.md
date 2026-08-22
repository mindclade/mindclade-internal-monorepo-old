# AI Gateway provider-edge runbook

This runbook is read-only by default. It does not authorize policy approval, budget mutation,
secret access, deployment, reconciliation, or a GitOps promotion.

## Triage order

1. Identify the workspace, endpoint alias, policy epoch, reservation ID, and request digest from
   metadata-only audit records. Never collect prompts, responses, bearer tokens, or provider keys.
2. Confirm the Rust proxy and Go API target health, then inspect the reservation lifecycle. A
   dispatched or reconciliation-pending reservation represents possibly consumed provider usage.
   Confirm the reservation subject is the expected `google-<sha256>` value and the authenticated
   control-plane actor is the dedicated gateway proxy; never substitute an email or raw token
   subject.
3. Compare the local endpoint snapshot with the authoritative resolved bundle: operation, route,
   connection reference, pricing version, maximum request, body limit, and trace/usage posture must
   match exactly.
4. Check Secure Web Proxy allow/deny logs and TLS errors. An unlisted host, missing interception
   CA, expired certificate, redirect, or direct-provider path is a security control firing, not a
   reason to open egress.
5. Check PostgreSQL sweep backlog, oldest pending age, audit/outbox lineage, and budget headroom.
   Restore admission only after durable state and monitoring agree.

| Symptom | Safe interpretation | Containment |
|---|---|---|
| 401/403 before reservation | caller token/audience, delegated-subject permission, or entitlement mismatch | keep the request denied; correct identity or approve a new bundle |
| 409 policy mismatch | local connection snapshot is stale or tampered | hold the endpoint; reconcile the approved snapshot, never ignore the epoch |
| 429 before dispatch | entitlement/workspace budget exhausted | preserve rejection; use two-person administration if a real policy change is required |
| provider error after dispatch | outcome can be billable | leave pending or max-charge; never release based only on client-visible failure |
| Secure Web Proxy denial | destination is not approved | verify exact host and policy evidence; do not add wildcard or direct egress |
| pending backlog grows | sweeper/database or provider accounting is unhealthy | stop new admission for the affected workspace and preserve durable evidence |

Rollback selects the last attested Rust image and matching policy snapshot through GitOps. A policy
rollback is a new monotonically increasing bundle, never a database rewrite. Recovery is complete
only after allowed/denied egress probes, no-overspend pressure, pending reconciliation, disruption,
metrics/alerts, and an empty GitOps diff are recorded against the recovered subjects.
