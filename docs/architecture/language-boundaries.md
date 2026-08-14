# Language boundaries

## Final ownership rule

```text
Go         fleet control plane and durable policy
Rust       online/runtime data plane and node execution
Python     scientific, model, training, inference, and evaluation numerics
TileLang   qualified accelerator kernels
TypeScript browser applications and generated web clients
```

This split follows responsibility and failure mode rather than language
preference.

## Go

Go owns durable, globally coordinated policy:

- tenancy, identity, entitlements, quotas, and usage;
- runs, jobs, attempts, leases, audit, webhooks, and transactional events;
- model, dataset, checkpoint, reference-database, and release catalog state;
- route/deployment policy and immutable route-snapshot publication;
- cluster scheduling, Kubernetes reconciliation, and global capacity policy;
- ingestion and preprocessing workflow state;
- signing execution/admission grants from durable policy.

Go does not own tensor batching, scientific parsing policy, model execution,
GPU memory allocation, artifact byte streaming, or node-local process
supervision.

## Rust

Rust owns the hot online and node-execution paths:

- runtime gateway, ticket validation, local route selection, request framing;
- SSE/streaming, cancellation, deadlines, response multiplexing, load shedding;
- runtime host and Python-process supervision;
- hierarchical node/service/worker/request resource budgets;
- local artifact and reference-database caches;
- high-throughput object-store, artifact, checkpoint, and dataset transfer;
- bounded untrusted-byte parsing and record framing;
- node agent, telemetry spool, and worker control/data IPC.

Rust does not recreate model, MSA, template-selection, objective, or optimizer
semantics.

## Python/PyTorch

Python owns scientific and numerical meaning:

- model architectures, parameter semantics, objectives, losses, optimizers;
- training state, topology plans, numerical reductions, checkpoint semantics;
- final tensor-aware batching, packing, shape buckets, KV/feature caches;
- diffusion sampling, model execution, confidence and ranking;
- biological normalization, curation, MSA filtering/pairing, template selection;
- ligand preparation, feature construction, evaluation, and scientific
  provenance.

Long-lived Python model workers are process-isolated behind versioned IPC.
PyO3 remains a leaf adapter for bounded parsing, manifest validation, digesting,
and buffer descriptors where measurements justify it.

## TileLang

TileLang owns accelerated GPU implementations only. PyTorch remains the semantic
reference. A kernel can be selected only when a qualification manifest matches
the operation, dtype, shape family, layout, target architecture, compiler
version, numerical tolerance, gradient requirements, and performance floor.
Unknown, revoked, or unqualified signatures fall back.

## TypeScript

TypeScript owns browser applications, design-system components, scientific
visualization, and generated public clients. Apps consume SDKs and contracts;
they never import service implementations.

## Batching boundary

Rust owns:

- admission queues and concurrency limits;
- coarse compatibility groups declared by signed model manifests;
- host/GPU reservation envelopes;
- backpressure, deadlines, cancellation, and response multiplexing.

Python owns:

- final tensor compatibility and batch formation;
- padding, packing, MSA/template/atom shape constraints;
- KV/feature-cache and CUDA-graph selection;
- diffusion/model-seed/sample scheduling;
- actual GPU execution.

This prevents model-specific compatibility logic from being implemented twice.

## Cross-language contracts

Canonical wire types live under `protocols/`. Large payloads are referenced by
artifact or local-buffer descriptors, not embedded in control messages. Golden
vectors verify Go, Rust, and Python agreement on identifiers, digests, faults,
resource versions, event envelopes, execution tickets, route snapshots,
worker commands/status, and manifests.
