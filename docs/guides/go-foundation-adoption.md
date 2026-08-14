# Go foundation adoption guide

## Purpose

`libs/go` is the mandatory mechanism layer for Mindclade's Go fleet-control
plane. It exists to eliminate correctness-sensitive duplication while keeping
service and domain policy outside the library tree.

Use it for reusable lifecycle, transport, persistence, security, telemetry,
and durable-coordination mechanisms. Keep tenancy rules, quotas, scheduling
policy, ingestion semantics, registry policy, route policy, and workflow state
under `control/` or the owning service.

## Standard dependency direction

```text
protocols/generated bindings
        ↓
libs/go foundations and mechanisms
        ↓
control domain engines
        ↓
services/control_plane composition roots
```

`libs/go` must never import `control/`, `services/`, model code, preprocessing,
or scientific data packages.

## Required process path

Every production Go process follows one path:

```text
load strict configuration
    → construct provider adapters
    → construct domain repositories and engines
    → assemble typed process dependencies
    → register mechanisms with servicekit/production.Builder
    → validate the role capability manifest
    → run through servicekit signal, health, drain, and shutdown lifecycle
```

The canonical composition code lives under:

```text
services/control_plane/internal/bootstrap/
services/control_plane/internal/foundation/
services/control_plane/internal/transport/
```

Process binaries under `services/control_plane/cmd/` are deliberately thin.
They select a stable role and pass a service-owned factory to the common
bootstrap path.

## Durable mutation pattern

A durable domain mutation that emits an event should use one SQL transaction:

```text
validate resource version and idempotency key
    → mutate domain rows
    → append audit record
    → append outbox record
    → commit
```

The dispatcher later claims outbox records with a fenced lease, publishes them
through `messaging`, and records completion or retry state. Consumers use the
inbox processor and a monotonic cursor so duplicate deliveries cannot produce
duplicate effects.

Use these packages:

```text
libs/go/idempotency
libs/go/idempotency/postgres
libs/go/resourceversion
libs/go/audit
libs/go/audit/postgres
libs/go/coordination/outbox
libs/go/coordination/outbox/postgres
libs/go/coordination/inbox
libs/go/coordination/cursor
libs/go/coordination/projector
libs/go/messaging
```

## Controllers, operators, and schedulers

Long-running reconcilers should use:

```text
servicekit/production        process capability and lifecycle validation
coordination/leadership      singleton leadership with fencing
coordination/workqueue       leased work, retry, heartbeat, dead-letter state
storage/lease                canonical lease and fencing contract
kubernetes/*                 client, controller, status, patch, finalizer helpers
retry                         explicit retry intent and shared budgets
observability                 bounded structured telemetry
```

A controller must stop claiming new work during drain, finish or release
existing claims within the shutdown budget, and reject stale fencing tokens.

## APIs and administrative processes

API-like processes should use:

```text
auth                         authentication and authorization contracts
audit                        immutable audit events
idempotency                  replay-safe mutation execution
pagination                   signed keyset cursors
resourceversion              optimistic-concurrency preconditions
signing                      detached signatures and key rotation
httpx / connectx / grpcx     standard transport behavior
servicekit/production        validated serving lifecycle
```

Do not create local error encodings, pagination tokens, signal handlers,
health frameworks, retry loops, or HTTP clients where a foundation package
already owns the mechanism.

## Ingestion coordinators

The Go ingestion coordinator owns durable workflow and source-snapshot state;
it does not parse scientific formats or implement curation policy.

```text
Go control/ingestion
    → creates source snapshot and stage DAG
    → uses cursor + workqueue + outbox + leases
    → schedules Rust transfer/parser workers
    → schedules Python curation and publication stages
```

The required package inventory is emitted directly by the scaffold:

```bash
go run ./services/control_plane/cmd/ingestion_controller --describe-profile
```

## Package promotion rule

Add a package to `libs/go` only when all are true:

1. At least two independent Go consumers need the mechanism.
2. The contract is transport- and domain-neutral.
3. The package has a clear layer and prohibited dependency directions.
4. It includes focused tests or a reusable conformance suite.
5. It has a Bazel target, README, and explicit ownership.
6. Moving it into the foundation reduces duplicated correctness or operations
   risk—not merely line count.

Never add catch-all `common`, `shared`, `helpers`, or `utils` packages.
