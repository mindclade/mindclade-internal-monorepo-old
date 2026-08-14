# ADR-0011: Standard durable Go coordination

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Go control plane

## Context

Transactional publication, duplicate-safe consumption, ordered projection,
long-running leased work, and singleton authority are required by APIs,
schedulers, controllers, operators, ingestion coordinators, projectors,
dispatchers, registries, webhooks, and administrative jobs. Service-local
implementations would differ in transactions, retries, fencing, drain, and
failure recovery.

## Decision

`libs/go/coordination` provides one set of durable mechanisms:

- `outbox`: transactional event append and fenced at-least-once dispatch;
- `inbox`: duplicate-safe transactional event application;
- `cursor`: monotonic compare-and-advance source/projector position;
- `projector`: bounded source + inbox + cursor + handler loop;
- `workqueue`: fenced claims, heartbeat, cancellation, retry, and dead letter;
- `leadership`: lease-backed singleton authority with fencing.

Memory/conformance adapters support tests. PostgreSQL adapters are the default
authoritative production persistence. Domain schemas, event meaning, workflow
policy, and handlers remain with their consumers.

## Consequences

- Domain mutation, audit, and outbox append can share one SQL transaction.
- Projector effects, inbox completion, and cursor advancement can be atomic.
- Stale workers/leaders cannot commit after replacement when consumers enforce
  the returned fence.
- Outbox, inbox, work queue, and broker delivery remain distinct mechanisms.

## Rejected alternatives

Service-local outbox tables/pollers, a universal ambiguous queue abstraction,
and claims of exactly-once delivery were rejected.

## Enforcement

Go architecture checks reject promoted service-local replacements. Role
capability profiles distinguish stores from active dispatchers/workers.
Conformance and failure-injection tests cover duplicates, stale tokens, lease
loss, retry exhaustion, transaction rollback, and bounded shutdown.
