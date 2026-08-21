# Training / Runtime / Telemetry

- **Status:** Target-state scaffold; no production capability is claimed by this file.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

Authoritative training contracts, core state machine, engine adapters, distributed plans, checkpoint orchestration, optimizers, runtime mechanisms, and task objectives. This path specializes that domain for **telemetry**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Materialization requirements

Before this scaffold boundary is treated as implemented, add:

- a named owner and reviewed stable contract;
- implementation with bounded resources, cancellation, and deterministic or
  explicitly statistical behavior;
- package-local tests plus required integration/numerical/security evidence;
- a Bazel target using the pinned Nix toolchain environment;
- explicit inputs, outputs, compatibility, failure, retry, and rollback rules;
- documentation of limits and non-responsibilities;
- `PRODUCTION_READINESS.md` evidence for deployment-facing code.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.

## MLflow projection

`exporters/mlflow.py` and `exporters/mlflow_tracing.py` are implemented mirror adapters. They
project immutable Mindclade run/dataset/artifact/model/evidence identity into MLflow while keeping
CAS, release, scheduling, and serving authority outside MLflow. Trace projection is payload-free:
only bounded `mindclade.*` identity attributes are accepted, and request inputs and outputs are
always omitted. Optional mode counts mirror failures without failing authoritative work; required
mode preserves the original client failure for workflows whose own policy explicitly requires the
projection.
