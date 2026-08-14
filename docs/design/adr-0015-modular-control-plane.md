# ADR-0015: Modular-monolith Go control plane

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Initial durable control-plane deployment

## Context

Tenancy, runs, jobs, orchestration, scheduling, registry, metadata, lineage,
usage, audit, routing, runtime authority, ingestion, and webhooks share strong
transactional and operational relationships. Splitting each domain into a
service before independent load/ownership exists would introduce distributed
transactions, event lag, migrations, and on-call burden without proven value.

## Decision

Begin with one Go control-plane code and persistence boundary containing strict
`control/` domain modules, repository interfaces, outbox/inbox boundaries, and
role-specific process entry points under `services/control_plane/cmd/`.
Processes may run as separate roles where resource or operational isolation is
useful, while retaining one shared contract, migration, bootstrap, and
mechanism foundation.

A domain is split into an independent service only when evidence demonstrates
independent scaling, availability, security, data residency, ownership, release
cadence, or failure-blast-radius requirements.

## Consequences

The initial system has simpler transactions, audit, outbox, migrations,
resource versions, and operations while preserving interfaces/events needed for
future extraction.

## Rejected alternatives

Service-per-domain symmetry and an unstructured all-in-one package were both
rejected.
