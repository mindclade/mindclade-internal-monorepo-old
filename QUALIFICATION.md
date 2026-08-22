# Mindclade · Qualification status

This archive distinguishes implemented source, local/offline qualification,
connected-provider qualification, and production promotion.

## Current repository-only verification (2026-08-21)

The root repository-home/common-document gate, top-level link hierarchy,
first-party proprietary header scan, and Cargo dependency-license scan pass.
The static presubmit passes its first 20 architecture and implementation gates,
then correctly blocks on an unapproved
`services/control_plane -> go.mindclade.dev/protocols/servicepolicy`
dependency. Until that dependency budget is reconciled, the complete static
lane is not qualified. These results are repository-only evidence and do not
replace connected provider, GPU, deployment, or production-promotion evidence.

Affected Bazel selection and the CPU nightly lane are implemented with
versioned evidence, owning Bazel tests, a real-graph fixture, operational
limits, and fail-closed Git/query behavior. They remain `implemented`, not
`qualified`: the exact GitHub check contexts, merge-group full run,
intentional-negative enforcement, scheduled nightly evidence, and 28-day
latency objective require connected observation.

## Implemented and locally qualified

- Full reusable Go foundation under `libs/go`.
- Layering, paved-road, and promoted-foundation static checks.
- Durable Go coordination: cursor, inbox, leadership, outbox, projector, and
  fenced work queue.
- Mandatory process assembly through `servicekit/production`.
- Modular control-plane bootstrap/config/foundation/transport seams.
- Representative Go durable-policy/domain contracts under `control/`.
- Runnable control-plane API, event-dispatcher, and ingestion-coordinator vertical slices.
- Bzlmod lock closure, full Bazel configured analysis, dependency-layer graph,
  and the complete non-manual Bazel test graph in the pinned local macOS/Nix environment.
- Complete target-state path materialization, accepted decision register, Go module cookbook, security documentation, and operating runbooks.
- Structured validation of 87 JSON, 185 TOML, 195 YAML/YML, 548 Markdown files, and internal relative links.

The exact local Go package set is stored in
`qualification/go/offline-safe-packages.txt`; normal tests, vet, and
race-enabled tests passed for all 111 entries.

## Go module closure

The root `go.sum` contains both module and `go.mod` checksums for all 18 direct public root
requirements, and now carries the transitive graph as well — 438 lines, up from the 36 that
covered only the direct set. The code/docs alignment gate checks the direct-requirement
invariant against `go.mod`; it deliberately does not assert a line count, because that number
moves with any dependency change and a gate that fails on it teaches people to edit the
number rather than look at the diff. Full transitive closure verification remains a
connected-lane gate (`go mod download all`, `go mod verify`, `go mod tidy -diff`) rather than
something inferred from the file's size. Connected CI runs
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

## Kernel lane

The kernel provider contract, PyTorch semantic references, execution-mode-safe
dispatch, TileLang source candidates, target legality, single-flight compile
cache, offline autotuning, compiler inspection, and schema-v2 paired
qualification/revocation records are implemented. The production workload
catalog contains 124 reciprocal inference/training pairs (248 exact requests).
Local CPU evidence covers API, numerical references, gradients, eligibility,
cache concurrency/failure/fork behavior, qualification policy, tool
orchestration, and scaffold integrity. Connected tests skip honestly off-GPU.

No TileLang signature is promoted by this implementation. The connected x86_64
Linux lane must use exactly TileLang `0.1.13` and apache-tvm-ffi
`>=0.1.11,<0.1.13`, compile the exact source/schedule, inspect generated
instructions, run the complete sanitizer suite and adversarial parity, benchmark
with synchronization, and bind results to artifact/toolchain/runtime-image and
device digests. Only CUDA `sm_90` is runtime-registered. CUDA `sm_100`,
`sm_120` and ROCm `gfx90a`, `gfx942`, `gfx950` remain source target models,
not local hardware claims.

## Model lane

Locally tested PyTorch leaves now cover dense/causal attention, rotary
embeddings, normalization, SwiGLU/feed-forward/residual components, a small
decoder-only LLM, and bounded trusted-digest `torch.export` packaging. This is
reference and contract evidence only. It is not end-to-end training,
generation/cache, checkpoint-migration, ONNX/AOTInductor, distributed,
quantized, accelerator-performance, or serving qualification.

## Explicit non-claims

The pinned Rust/Bazel/Nix toolchain is available and the local Cargo and Bazel
suites pass, including the `runtime_gateway` / `runtime_host` cores. The Rust
presubmit also passes cargo-deny, compatibility, six local failure-injection
scenarios, and two portable performance measurements. This is not
connected-provider, Linux unsafe-code, remote-execution, sanitizer, fuzz/Miri,
or complete hardware/provider performance evidence. Python configuration,
preprocessing contracts, cross-language fixtures, and the kernel
source/qualification boundary are implemented and locally tested; broader
Python numerical systems, TileLang hardware qualification, TypeScript apps,
infrastructure, provider adapters, and product code remain scaffolded or
partially implemented unless local evidence states otherwise. Source presence
never implies numerical parity, security qualification, performance
qualification, or production readiness.

## Final hardening promotion boundary (2026-08-13)

All ten post-architecture hardening items are represented by executable source, policy, or qualification machinery: Rust lock/toolchain gating, supply-chain policy, rolling compatibility, failure injection, performance budgets, node diagnostics, resource-budget observability, finished artifact-GC semantics, canonical workload envelopes, and golden vertical release slices. Affected-test selection and component ownership metadata are active source invariants; connected required-check enforcement remains a separate governance qualification gate.

The canonical Rust presubmit and offline qualification pass. `qualified` and
`production` maturity still require connected Linux/Bazel/Nix release evidence,
real provider integrations, the remaining hardware performance measurements,
and release/security evidence.
