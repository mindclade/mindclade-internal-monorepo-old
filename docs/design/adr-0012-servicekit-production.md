# ADR-0012: Mandatory `servicekit/production` lifecycle

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Every production Go process

## Context

Independent process implementations of configuration, provider lifecycle,
health, signal handling, background goroutines, readiness, drain, and shutdown
would make failure behavior inconsistent and would prevent repository-wide
qualification.

## Decision

Every production Go executable follows one composition path:

```text
service-owned provider factory
    -> typed immutable dependencies
    -> servicekit/production.Builder
    -> stable role/capability validation
    -> canonical lifecycle stages
    -> readiness, drain, cancellation, reverse shutdown
```

Passive capabilities are declared only after concrete dependencies exist.
Lifecycle-owning mechanisms are registered at their canonical stage. Domain
engines are explicit work components. Command roots neither construct providers
nor own signals. Scaffold factories fail closed; `--describe-profile` remains
available for diagnostics and qualification.

## Consequences

- A process cannot appear ready while mandatory role capabilities are absent.
- Readiness is withdrawn before drain; active loops stop claiming new work.
- Background work has an owner and bounded completion/join budget.
- Provider construction and domain policy remain outside servicekit.

## Rejected alternatives

A broad dependency-injection framework, direct `servicekit.New` from commands,
and process-specific signal/health frameworks were rejected.

## Enforcement

Static checks inspect command roots and imports. Service conformance tests cover
invalid configuration, startup order, readiness, drain, SIGTERM, reverse
shutdown, task leaks, telemetry flush, redaction, and bounded queues.
