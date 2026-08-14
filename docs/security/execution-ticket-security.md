# Execution-ticket and admission-grant security

## Purpose

Signed authority lets Rust gateways, hosts, node agents, and workers execute
within Go-owned policy without a synchronous control-plane lookup per request.

## Required signed claims

```text
ticket/grant ID and issuer
tenant and workspace
request, run, job, stage, and attempt identity as applicable
model/runtime/engine bundle digests
allowed artifact prefixes and output namespace
execution class and accelerator capability
CPU, memory, disk, GPU, queue, output, and time budgets
not-before, issued-at, deadline, and expiration
policy, route, entitlement, and revocation epochs
fencing token and retry/attempt identity
key ID, algorithm, and signature
```

## Verification order

1. Decode with strict size/schema limits.
2. Select an allowed key/algorithm and verify canonical signed bytes.
3. Validate time window and acceptable clock skew.
4. Validate minimum accepted policy/revocation/route epochs.
5. Validate tenant, service, deployment, capability, and artifact scope.
6. Reserve local resources before substantial allocation.
7. Bind all status/artifact commits to the fencing token and attempt.

## Failure behavior

Invalid, unsigned, expired, revoked, out-of-scope, over-budget, or stale-fenced
authority is rejected. Verification is never bypassed to restore availability.
Already-admitted work follows explicit policy during control-plane outages.
