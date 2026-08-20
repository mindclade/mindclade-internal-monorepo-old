# Training / Distributed / Topology

- **Status:** Target-state scaffold; no production capability is claimed by this file.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

The rank arrangement a run executes on: `mesh.py`, `groups.py`, `ranks.py`, `placements.py`, `parallel_dims.py`, `replica_groups.py`, `world.py`, `execution_scope.py` and `topology.py`, with `validation.py` guarding the shapes. This package describes where a rank sits; `communication/` owns what it sends.

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
