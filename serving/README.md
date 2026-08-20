<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Model serving

> **Maturity:** Mixed; serving contracts and the Rust runtime core are
> implemented, while broader engines and deployment qualification remain
> component-specific.
> **Primary implementation:** Python/PyTorch inference engines and Rust runtime
> mechanisms.

`serving/` owns reusable model-loading, batching, sampling, safety, rollout,
and inference-runtime behavior. Network and process composition stays under
[`services/`](../services/).

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Requests, responses, model bundles, descriptors, and runtime manifests |
| [`runtime/`](runtime/) | Rust gateway/host runtime core and outage behavior |
| [`model_worker/`](model_worker/) | Python model loading, execution, batching, precision, and health |
| [`batch/`](batch/) | Batch inference jobs, queues, retries, artifacts, and telemetry |
| [`rollouts/`](rollouts/) | Policy synchronization, sampling, actors, and trajectories |
| [`safety/`](safety/) | Request and output policy, screening, audit, and validation |
| [`testing/`](testing/) | Fakes, fixtures, goldens, and load-test support |

## Boundary

- Reusable inference behavior belongs here; sockets, signals, provider
  construction, and deployment lifecycle belong in
  [`services/`](../services/).
- Model architecture and weights contracts originate in
  [`models/`](../models/).
- Admission, routing, tenancy, and release authority remain in the Go control
  plane.
- Runtime requests and artifacts must remain bounded, cancellable, observable,
  and tenant-scoped.

## Start here

- [Serving architecture](../docs/architecture/serving.md)
- [Runtime data-plane architecture](../docs/architecture/runtime-data-plane.md)
- [`contracts/README.md`](contracts/README.md) for stable interfaces
- [`runtime/README.md`](runtime/README.md) for the implemented Rust core

Consult [`components.toml`](../components.toml) and
[`QUALIFICATION.md`](../QUALIFICATION.md) for component-specific readiness.
