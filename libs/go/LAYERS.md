# Go library layering

Copyright 2026 Mindclade. All rights reserved. Confidential and proprietary.

`libs/go` contains production mechanisms that are expected to be reused across
multiple Go processes. It does not contain tenant, scheduler, registry,
ingestion, model, or product policy.

## Layer 0 — Foundations

```text
clock
faults
identifiers
```

These packages use only the Go standard library and define the lowest-level
contracts used throughout the platform.

## Layer 1 — Stable contracts and portable primitives

```text
audit                   -> auth, clock, faults, identifiers, requestmeta
auth                    -> faults, identifiers
idempotency             -> clock, faults, identifiers, requestmeta
messaging               -> faults, identifiers, requestmeta
pagination              -> clock, faults, identifiers, signing
requestmeta             -> faults, identifiers
resourceversion         -> faults, identifiers
signing                 -> clock, faults
storage/blob            -> narrow blob-store contract
storage/cache           -> narrow cache contract
storage/lease           -> lease and fencing contract
storage/sql/transaction -> explicit transaction context contract
```

Layer 1 owns stable security, request lineage, audit, replay safety, delivery,
optimistic concurrency, signing, pagination, and narrow infrastructure
contracts. Contract roots do not import provider clients or service policy.

## Layer 2 — Runtime and durable coordination mechanisms

```text
config                  -> clock, faults, identifiers
observability           -> auth, clock, faults, requestmeta
retry                   -> clock, faults
servicekit              -> clock, faults
servicekit/production   -> faults, servicekit
coordination/cursor     -> clock, faults, identifiers
coordination/inbox      -> idempotency, transaction
coordination/leadership -> clock, retry, servicekit, lease
coordination/outbox     -> clock, retry, servicekit, lease
coordination/projector  -> cursor, inbox, servicekit
coordination/workqueue  -> clock, retry, servicekit
```

Layer 2 executes reusable behavior and coordination state machines. It does not
import HTTP, Connect, gRPC, Kubernetes, or provider-specific storage packages.

## Layer 3 — Infrastructure adapters

```text
kubernetes/*                     -> Layers 0-2 plus controller-runtime/client-go
messaging/memory                 -> deterministic bounded local/test adapter
messaging/pubsub                 -> at-least-once provider facade and adapter
storage/blob/{gcs,memory}        -> blob contract plus provider client
storage/cache/{redis,memory}     -> cache contract plus provider client
storage/lease/{postgres,memory}  -> lease contract plus provider client
storage/sql/postgres             -> database/sql lifecycle and qualification
storage/sql/migrate              -> checksummed forward migration runner
audit/postgres
idempotency/postgres
coordination/*/postgres          -> transaction-aware PostgreSQL adapters
coordination/*/memory            -> deterministic local/conformance adapters
```

The root `storage` directory is a namespace rather than a universal storage
interface. Consumers import the precise contract they need.

## Layer 4 — Transport adapters

```text
httpx                 -> Layers 0-2
httpx/outbound        -> policy-enforcing outbound HTTP
connectx              -> Layers 0-2, internal/rpcfaults
grpcx                 -> Layers 0-2, internal/rpcfaults
internal/rpcfaults    -> faults, requestmeta
```

Transport adapters translate shared contracts into protocol behavior. Lower
layers never import Layer 4.

## Layer 5 — Consumers

Layer 5 is outside `libs/go`:

```text
control/
services/
operators/
workers/
```

Consumers compose reusable mechanisms and own domain policy, repositories,
generated APIs, orchestration, reconciliation, and executable entry points.

## Public mechanism inventory

```text
audit
auth
clock
config
connectx
coordination/cursor
coordination/inbox
coordination/leadership
coordination/outbox
coordination/projector
coordination/workqueue
faults
grpcx
httpx
httpx/outbound
idempotency
identifiers
kubernetes
messaging
messaging/pubsub
observability
pagination
requestmeta
resourceversion
retry
servicekit
servicekit/production
signing
storage/blob
storage/cache
storage/lease
storage/sql/migrate
storage/sql/postgres
storage/sql/transaction
```

Provider and transport subpackages are public only where they are intentional
composition-root adapters. `internal/rpcfaults` is private. Packages ending in
`test`, `*test`, or dedicated conformance namespaces are test-only.

## Prohibited dependency directions

```text
Layer 0 -> Layer 1+
Layer 1 contracts -> Layer 2+ mechanisms
Layer 2 -> provider-specific Layer 3 adapters
Layer 3 -> Layer 4 transports
libs/go -> Layer 5 consumers
production packages -> test-only packages
transport-neutral packages -> transport adapters
```

Exceptions require an architecture review and an explicit package-boundary
change rather than a convenience import.
