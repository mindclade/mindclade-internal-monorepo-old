# Qualification status

This archive distinguishes implemented source, local/offline qualification,
connected-provider qualification, and production promotion.

## Implemented and locally qualified

- Full reusable Go foundation under `libs/go`.
- Layering, paved-road, and promoted-foundation static checks.
- Durable Go coordination: cursor, inbox, leadership, outbox, projector, and
  fenced work queue.
- Mandatory process assembly through `servicekit/production`.
- Modular control-plane bootstrap/config/foundation/transport seams.
- Representative Go durable-policy/domain contracts under `control/`.
- Runnable control-plane API, event-dispatcher, and ingestion-coordinator vertical slices.
- Complete target-state path materialization, accepted decision register, Go module cookbook, security documentation, and operating runbooks.
- Structured validation of 87 JSON, 185 TOML, 195 YAML/YML, 548 Markdown files, and internal relative links.

The exact local Go package set is stored in
`qualification/go/offline-safe-packages.txt`; normal tests, vet, and
race-enabled tests passed for all 111 entries.

## Go module closure

The root `go.sum` now contains both module and `go.mod` checksums for all 18 direct
public root requirements (36 checksum lines). The code/docs alignment gate checks
this invariant against `go.mod`. The local sandbox cannot download the complete
transitive module source graph, so full transitive closure remains a connected-lane
gate rather than being fabricated from unrelated lockfiles. Connected CI runs
`go mod download all`, `go mod verify`, and `go mod tidy -diff` before provider
qualification.

## Connected provider lane

Connected CI must resolve the pinned real dependencies and run conformance
against ephemeral or isolated providers:

```text
PostgreSQL  transaction, migration, audit, idempotency, lease, cursor, outbox, queue
Redis       cache scripts, TTL/version behavior, reconnect/failure classification
GCS         range I/O, preconditions, checksums, resumable transfer, cancellation
Pub/Sub     ordering, redelivery, ack extension, bounded shutdown
Kubernetes  clients, watches, reconciliation, conflicts, JobSet/Kueue, drain
Connect     interceptors, authentication, faults, health, reflection, tracing
gRPC        unary/stream interceptors, TLS/mTLS, faults, health, reflection, tracing
OpenTelemetry propagation, cardinality/redaction, exporter failure, bounded flush
```

## Build/release lane

Promotion additionally requires:

- Bazel/Bzlmod graph and lock closure;
- Nix toolchain and remote execution image parity;
- hermeticity/no-host-leakage checks;
- OCI image, SBOM, provenance, signatures, and vulnerability/license evidence;
- deployment smoke, drain, rollback, failure injection, and SLO evidence;
- service-specific security and production-readiness approval.

## Explicit non-claims

The Rust foundation and the `runtime_gateway` / `runtime_host` cores now contain
substantive source implementations for the optimization program, but they have
not been compiled or runtime-qualified in this execution environment because the
pinned Rust/Bazel/Nix toolchain is unavailable. Python configuration,
preprocessing contracts, and cross-language fixtures are implemented and locally
tested; broader Python numerical systems, TileLang kernels, TypeScript apps,
infrastructure, provider adapters, and product code remain scaffolded or
partially implemented unless local evidence states otherwise. Source presence
never implies numerical parity, security qualification, performance
qualification, or production readiness.

## Final hardening promotion boundary (2026-08-13)

All ten post-architecture hardening items are represented by executable source, policy, or qualification machinery: Rust lock/toolchain gating, supply-chain policy, rolling compatibility, failure injection, performance budgets, node diagnostics, resource-budget observability, finished artifact-GC semantics, canonical workload envelopes, and golden vertical release slices. Affected-test selection and component ownership/enforcement metadata are also active presubmit invariants.

Offline qualification passes. `qualified` and `production` maturity still require connected evidence for the pinned Rust 1.97.1 toolchain, a Cargo-generated committed lockfile, real provider integrations, hardware performance measurements, and release/security evidence.

