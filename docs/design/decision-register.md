# Architecture decision register

The canonical cross-system synthesis of these decisions is
[`../architecture/system-design-reference.md`](../architecture/system-design-reference.md).
[`../architecture/system-design-traceability.md`](../architecture/system-design-traceability.md)
maps each design area to source and qualification. ADRs remain the immutable
history for authority-changing choices.

Accepted decisions are immutable records. A later ADR supersedes rather than
silently rewrites an earlier decision.

| Decision | Title | Status |
|---|---|---|
| ADR-0001 | [Bazel with Bzlmod owns the build graph](adr-0001-bazel-bzlmod.md) | Accepted |
| ADR-0002 | [Nix owns toolchains and execution environments](adr-0002-nix-owned-toolchains.md) | Accepted |
| ADR-0003 | [Polyglot responsibility boundaries](adr-0003-polyglot-boundaries.md) | Accepted |
| ADR-0004 | [Content-addressed immutable artifacts](adr-0004-content-addressed-artifacts.md) | Accepted |
| ADR-0005 | [Signed execution tickets and route snapshots](adr-0005-execution-tickets.md) | Accepted |
| ADR-0006 | [Rust owns the runtime data plane](adr-0006-rust-runtime-data-plane.md) | Accepted |
| ADR-0007 | [Python owns numerical and scientific authority](adr-0007-python-numerical-authority.md) | Accepted |
| ADR-0008 | [TileLang kernels require qualification](adr-0008-qualified-tilelang-kernels.md) | Accepted |
| ADR-0009 | [Layered Go mechanism foundation](adr-0009-go-library-layers.md) | Accepted |
| ADR-0010 | [Services are deployable composition roots](adr-0010-services-are-composition-roots.md) | Accepted |
| ADR-0011 | [Standard durable Go coordination](adr-0011-durable-go-coordination.md) | Accepted |
| ADR-0012 | [Mandatory servicekit production lifecycle](adr-0012-servicekit-production.md) | Accepted |
| ADR-0013 | [Durable ingestion and scientific preprocessing](adr-0013-durable-preprocessing.md) | Accepted |
| ADR-0014 | [Explicit protocol source-of-truth matrix](adr-0014-protocol-authority.md) | Accepted |
| ADR-0015 | [Modular-monolith Go control plane](adr-0015-modular-control-plane.md) | Accepted |
| ADR-0016 | [Rust/Python inference batching boundary](adr-0016-rust-python-batching-boundary.md) | Accepted |
| ADR-0017 | [Single checkpoint authority](adr-0017-checkpoint-authority.md) | Accepted |
| ADR-0018 | [Evidence-based scaffold materialization](adr-0018-scaffold-materialization-policy.md) | Accepted |
| ADR-0019 | [Cohesive Rust runtime consolidation](adr-0019-rust-runtime-consolidation.md) | Accepted |
| ADR-0020 | [Unified ticketed stage-worker protocol](adr-0020-unified-stage-worker-protocol.md) | Accepted |
| ADR-0021 | [Machine-readable component maturity](adr-0021-component-maturity-policy.md) | Accepted |
| ADR-0022 | [Artifact/reference-data/evidence identity](adr-0022-artifact-reference-database-evidence.md) | Accepted |
| ADR-0023 | [Resolved config and dependency budgets](adr-0023-resolved-config-and-dependency-budgets.md) | Superseded in part by ADR-0024 |
| ADR-0024 | [Dependency layering over counts](adr-0024-dependency-layering-over-counts.md) | Accepted |
| ADR-0025 | [Exact training-service composition layer](adr-0025-training-service-composition-layer.md) | Accepted |
| ADR-0026 | [Workload identity and node placement stay distinct](adr-0026-workload-envelope-identity-and-placement.md) | Accepted |
| ADR-0027 | [Kernel-enforced syscall confinement](adr-0027-kernel-enforced-syscall-confinement.md) | Accepted |
| ADR-0028 | [Declared protobuf mirrors bound to the descriptor](adr-0028-declared-protobuf-mirrors.md) | Accepted |
| ADR-0029 | [Orchestration and scheduling boundaries](adr-0029-orchestration-and-scheduling-boundaries.md) | Accepted |

## Operating policies derived from these decisions

- one root dependency graph per internal language ecosystem unless an SDK is
  independently published;
- one canonical resolved configuration and digest per run/process;
- unit tests colocated with packages and top-level tests reserved for
  cross-boundary qualification;
- evaluation is independent of training and can qualify checkpoints, bundles,
  or endpoints;
- OCI images are Bazel outputs built on Nix-produced immutable bases;
- reference databases are immutable promoted snapshots staged to local cache;
- runtime behavior under control-plane outage is bounded and fail-closed;
- service decomposition requires measured triggers rather than directory
  symmetry.
- processes that handle untrusted input install kernel-enforced syscall
  confinement at process entry, or refuse to start.
