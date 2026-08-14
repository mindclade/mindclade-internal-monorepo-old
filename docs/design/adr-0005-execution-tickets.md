# Use signed, bounded execution authority

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Rust gateways and workers must execute without a synchronous Go policy/database lookup for every request while preserving tenancy, quotas, artifact scope, deadlines, release policy, and revocation.

## Decision

The Go control plane publishes immutable route/policy snapshots and signs admission grants or execution tickets. Tickets bind tenant/workspace, job/request, model/runtime bundle digests, artifact grants, resource budgets, deadline, policy/route epochs, fencing token, issue/expiry time, key ID, and signature. Rust validates them locally.

## Consequences

- Online grants are session/budget scoped where practical, not necessarily minted per request.
- Stale or duplicate workers cannot commit with an old fence.
- Expired or revoked authority drains existing work and rejects new work according to policy.

## Enforcement

- Cross-language golden vectors cover canonical signing bytes.
- Revocation epoch and key rotation are tested independently of route freshness.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
