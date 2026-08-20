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

When a role declares singleton authority, the elector owns the active
component's `Run` loop through `leadership.GateComponent`; the same loop must
not also be started independently by `servicekit`. Standby replicas retain
health probes and lifecycle hooks but execute no singleton work. Components
whose run loop is single-use set `ExitOnLeadershipLoss`, so loss cancels the
work and terminates the process for a clean restart instead of attempting to
reuse a canceled worker, projector, or controller manager.

Memory/conformance adapters support tests. PostgreSQL adapters are the default
authoritative production persistence. Domain schemas, event meaning, workflow
policy, and handlers remain with their consumers.

## Consequences

- Domain mutation, audit, and outbox append can share one SQL transaction.
- Projector effects, inbox completion, and cursor advancement can be atomic.
- Stale workers/leaders cannot commit after replacement when consumers enforce
  the returned fence.
- Standby singleton roles cannot process queue items, projections, or
  reconciliations before acquiring their lease.
- A leader that cannot renew fails closed and is restarted; Kubernetes process
  supervision, rather than an in-process second run, owns recovery.
- Outbox, inbox, work queue, and broker delivery remain distinct mechanisms.

## Rejected alternatives

Service-local outbox tables/pollers, a universal ambiguous queue abstraction,
and claims of exactly-once delivery were rejected.

## Enforcement

Go architecture checks reject promoted service-local replacements. Role
capability profiles distinguish stores from active dispatchers/workers.
Conformance and failure-injection tests cover duplicates, stale tokens, lease
loss, fail-stop leader-managed components, retry exhaustion, transaction
rollback, and bounded shutdown.
