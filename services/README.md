# Deployable services

`services/` contains executable composition roots. It never owns reusable
scientific/model logic or generic infrastructure mechanisms.

## Authority split

| Service | Language | Responsibility |
|---|---|---|
| `control_plane` | Go | durable tenancy, runs/jobs, orchestration, scheduling, registry, metadata, audit, quotas, route/ticket authority |
| `runtime_gateway` | Rust | online network boundary, local ticket validation, admission, streaming, cancellation, load shedding |
| `runtime_host` | Rust | Python process/model-slot supervision, node resource reservation, IPC, drain |
| `node_agent` | Rust | node cache, transfer, external tool execution, resource monitoring, diagnostics |
| `artifact_proxy` | Rust | tenant-scoped content-addressed artifact byte access, ranges, grants, verification |
| `workers/*` | Rust and/or Python | thin adapters around ingestion, preprocessing, model, evaluation, training, rollout, and simulation engines |

Services consume `protocols/`, `libs/`, `control/`, `data/`, `preprocessing/`,
`models/`, `training/`, `evaluation/`, and `serving/` as appropriate. Apps use
SDKs/contracts and never import service internals.

Every service README documents boundary, dependencies, determinism, resource
limits, lifecycle, failure behavior, and explicit limitations.
`PRODUCTION_READINESS.md` is a promotion checklist/evidence index, not a claim
that a scaffold process is ready.
