# Go service golden path

## Composition roots

Go executables are deployable adapters, not reusable domain packages. A
process binary selects a stable role and delegates all construction to a
service-owned factory:

```go
package main

import "go.mindclade.dev/services/control_plane/internal/bootstrap"

func main() {
    bootstrap.Main(
        bootstrap.RoleAPI,
        newProductionFactory(),
    )
}
```

The checked-in scaffold uses `bootstrap.UnconfiguredFactory` so incomplete
binaries fail closed rather than accidentally entering a release. Replace that
factory only after its provider adapters, domain engines, health checks,
resource limits, and qualification evidence are implemented.

## Reference integration 1: control-plane API

The API profile requires the shared lifecycle, strict configuration, IDs,
request metadata, observability, retry, database and transaction ownership,
authentication, authorization, audit, idempotency, signed pagination,
resource-version preconditions, signing, an outbox store, and HTTP or gRPC.

Inspect the exact contract with:

```bash
go run ./services/control_plane/cmd/api --describe-profile
```

Relevant implementation paths:

```text
services/control_plane/cmd/api/main.go
services/control_plane/internal/bootstrap/
services/control_plane/internal/foundation/
services/control_plane/internal/transport/
libs/go/servicekit/production/
```

The `foundation.Dependencies` aggregate contains mechanisms and adapters only.
Generated handlers and domain services remain owned by the API factory.

## Reference integration 2: ingestion coordinator

The ingestion coordinator combines durable coordination with domain-owned
workflow logic:

```bash
go run ./services/control_plane/cmd/ingestion_controller --describe-profile
```

Its standard mechanisms include:

```text
PostgreSQL transactions and audit
idempotency
content-addressed blob storage
cache
lease and fencing
Kubernetes client/admission adapter
work queue
source cursor
leadership
messaging
resource versions
transactional outbox
```

Domain logic lives under `control/ingestion/`. Scientific parsing, MSA and
template semantics, curation, and model features remain in Rust or Python as
defined by the language-boundary ADRs.

## Lifecycle order

`servicekit/production` enforces this order:

```text
foundation      observability
infrastructure  database, migrations, Kubernetes manager
coordination    leadership, outbox dispatcher
work            scheduler, controller, projector, domain workers
serving         HTTP, Connect, gRPC
```

Shutdown reverses the order. Components enter drain before their run contexts
are canceled, allowing listeners and claim loops to stop accepting work while
already-admitted operations finish within bounded budgets.

## Provider construction

Provider clients belong in the service factory. Prefer the approved adapters:

```text
storage/sql/postgres
storage/sql/transaction
storage/sql/migrate
audit/postgres
idempotency/postgres
coordination/*/postgres
storage/blob/gcs
storage/cache/redis
storage/lease/postgres
messaging/pubsub
httpx/outbound
kubernetes/*
```

Memory adapters are for deterministic tests and local examples. Production
factories must not silently fall back to them.

## Qualification checklist

Before replacing `UnconfiguredFactory`, require:

- role capability manifest passes;
- configuration rejects unknown and missing fields;
- every active loop is owned by `servicekit`;
- readiness fails before dependencies are ready and during drain;
- shutdown completes within its budget;
- outbox/inbox, work queue, and leases pass provider conformance;
- faults are rendered without internal causes or sensitive fields;
- request, correlation, causation, and trace metadata propagate end to end;
- provider integration tests run against pinned real dependencies;
- Bazel and Nix toolchain manifests match;
- `PRODUCTION_READINESS.md` links the evidence.
