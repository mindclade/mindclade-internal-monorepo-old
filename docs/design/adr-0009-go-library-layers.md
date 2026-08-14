# Keep a layered Go mechanism foundation

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Control-plane APIs, schedulers, controllers, operators, ingestion coordinators, projectors, dispatchers, registries, and administrative jobs would otherwise duplicate lifecycle, retry, errors, identity, audit, coordination, storage, and transport behavior.

## Decision

`libs/go` owns layered domain-neutral mechanisms. Durable outbox/inbox/cursor/projector/workqueue/leadership primitives and one production service assembly path are shared. Domain policy remains under `control/`; executable wiring remains under `services/`.

## Consequences

- Broad consumption is increased by conformance suites and paved-road enforcement, not generic abstractions.
- A new shared package requires multiple independent consumers and clear layering.
- Catch-all common/shared/helpers/utils packages are forbidden.

## Enforcement

- Bazel visibility and Go analysis enforce dependency directions.
- Every Go process publishes a role/capability manifest and uses `servicekit/production`.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
