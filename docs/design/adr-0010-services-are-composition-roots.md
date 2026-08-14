# Services are deployable composition roots only

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Mixing reusable model, scientific, or domain policy into service directories makes testing, reuse, ownership, and later deployment decomposition difficult.

## Decision

`services/` contains process entry points, provider construction, generated transport binding, resource configuration, lifecycle wiring, and production-readiness evidence. Reusable Go policy lives under `control/`; scientific/model/runtime engines live in their owning top-level domains or libraries.

## Consequences

- Apps consume SDKs/contracts rather than importing services.
- A service split occurs only for proven scaling, availability, security, ownership, or release-cadence reasons.
- Every service documents limits, drain, failure behavior, and non-responsibilities.

## Enforcement

- Dependency checks prevent service imports from reusable packages.
- Promotion requires completed service-specific readiness evidence.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
