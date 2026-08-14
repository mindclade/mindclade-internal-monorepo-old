# Runbook: execution-ticket key rotation

## Planned rotation

1. Generate/import the new signing key in the approved KMS/HSM boundary.
2. Publish a signed verifier key-set snapshot containing old and new key IDs,
   validity windows, algorithms, and policy epoch.
3. Wait for all Rust gateways/hosts/agents to acknowledge the new verifier set.
4. Begin issuing new tickets/grants with the new key ID.
5. Retain the old verifier through the maximum authority lifetime plus clock
   skew/drain margin.
6. Retire the old verifier and publish a new minimum accepted key/policy epoch.

## Emergency compromise

- Stop issuing from the compromised key immediately.
- Advance the revocation/policy epoch and publish an emergency verifier set.
- Reject new and cached authority signed only by the revoked key, even before
  ordinary expiry.
- Drain affected sessions/work where policy requires it.
- Audit all tickets issued by the key and investigate artifact/status commits.

## Invariants

Canonical signed bytes, key ID, algorithm allowlist, issued-at/expiry, policy
and route epochs, artifact scopes, budgets, and fencing token must all verify.
No service may disable signature verification to restore availability.

## Exit criteria

All runtime components use the intended key set, no authority remains accepted
past its policy window, issuance and verification metrics are normal, and the
rotation evidence is attached to the security release record.
