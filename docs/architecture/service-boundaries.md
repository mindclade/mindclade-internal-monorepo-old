# Service boundaries

`services/` contains deployable composition roots. It never owns reusable
scientific, model, training, data, or durable-coordination logic.

## Target service families

| Service | Language | Boundary |
|---|---|---|
| `control_plane` | Go | Durable tenancy, runs, jobs, registry, scheduling, ingestion, audit, usage, routes, webhooks |
| `runtime_gateway` | Rust | Online network face, ticket validation, local admission, streaming, cancellation |
| `runtime_host` | Rust | Python worker supervision, model slots, local budgets, drain, control/data IPC |
| `artifact_proxy` | Rust | Tenant-scoped content-addressed byte streaming, ranges, digests, signed URLs |
| `node_agent` | Rust | Node cache, subprocess supervision, transfer, resource enforcement, diagnostics |
| `workers/*` | Python and/or Rust | Thin broker/job adapters around scientific, training, evaluation, or data engines |

Go control-plane roles may be deployed independently while sharing one domain
and persistence model. They do not copy policy or coordination code.

## Required process properties

Every production service documents and tests:

- owner, contract, dependencies, and non-responsibilities;
- configuration sources, schema, digest, and secret handling;
- bounded request, queue, concurrency, memory, disk, process, and network limits;
- readiness, liveness, drain, cancellation, stop, and telemetry-flush behavior;
- authentication, authorization, tenant isolation, audit, and idempotency;
- retry, deadline, fencing, duplicate-delivery, and partial-failure semantics;
- SLOs, dashboards, alerts, runbooks, and explicit limitations;
- Bazel build, OCI image, SBOM, provenance, signatures, and rollback evidence.

## Go composition path

```text
cmd/<role>/main.go
  -> service-owned provider factory
  -> services/control_plane/internal/foundation.Dependencies
  -> servicekit/production.Builder
  -> validated role profile and canonical lifecycle stages
  -> RunWithSignals / bounded shutdown
```

Commands fail closed while an unconfigured factory is present.

## Rust composition path

Rust services use one Tokio runtime, owned task trees, bounded blocking pools,
Tower limits/timeouts/load shedding, versioned Tonic/Prost control contracts,
explicit node-wide resource reservations, and structured drain. Long-lived
Python workers are external supervised processes.

## Worker rule

A worker adapter receives a signed execution ticket, validates scope and
fencing, resolves immutable artifacts/snapshots, calls its owning engine, emits
status with sequence numbers, stages outputs, and commits atomically. The worker
must not contain the reusable engine itself.
