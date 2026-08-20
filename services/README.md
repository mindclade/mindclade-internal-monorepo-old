<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Service boundaries](../docs/architecture/service-boundaries.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Deployable services

> **Maturity:** Mixed; several control-plane and Rust runtime cores are
> implemented, while provider, environment, and release qualification remains
> service-specific.
> **Primary implementation:** Go, Rust, and Python.

`services/` contains executable composition roots. Services assemble domain and
library behavior with providers, lifecycle, health, telemetry, and deployment
configuration; they do not own reusable scientific/model logic or generic
infrastructure mechanisms.

## Service map

| Service | Language | Responsibility |
| --- | --- | --- |
| [`control_plane/`](control_plane/) | Go | Durable tenancy, runs, orchestration, scheduling, registry, metadata, audit, quotas, and route/ticket authority |
| [`runtime_gateway/`](runtime_gateway/) | Rust | Online network boundary, local ticket validation, admission, streaming, cancellation, and load shedding |
| [`runtime_host/`](runtime_host/) | Rust | Python process/model-slot supervision, node reservation, IPC, and drain |
| [`node_agent/`](node_agent/) | Rust | Node cache, transfer, external-tool execution, resource monitoring, and diagnostics |
| [`artifact_proxy/`](artifact_proxy/) | Rust | Tenant-scoped content-addressed artifact access, ranges, grants, and verification |
| [`workers/`](workers/) | Rust and Python | Thin ingestion, preprocessing, model, evaluation, training, rollout, and simulation adapters |
| [`studio/`](studio/) | Service boundary | Studio-facing service composition reserved by the platform blueprint |
| [`go_vanity/`](go_vanity/) | Go/HTTP | Vanity import metadata surface for public Go module paths |

## Boundary

- Services consume [`protocols/`](../protocols/), [`libs/`](../libs/), domain
  packages, and reusable engines; they do not fork those contracts locally.
- Provider clients, server/process lifecycle, health, drain, and deployment
  composition belong here.
- Apps consume SDKs and public contracts; they never import service internals.
- Every resource, queue, stream, request, retry, and shutdown path must be
  bounded and observable.

## Start here

- [Service-boundary architecture](../docs/architecture/service-boundaries.md)
- [Runtime data plane](../docs/architecture/runtime-data-plane.md)
- [Go service golden path](../docs/guides/go-service-golden-path.md)
- [Rust runtime foundation](../docs/guides/rust-runtime-foundation.md)

Every service README documents responsibility, dependencies, determinism,
resource limits, lifecycle, failure behavior, and explicit limitations.
`PRODUCTION_READINESS.md` is an evidence index and promotion checklist, not a
readiness claim by itself.
