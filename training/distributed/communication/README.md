# Training / Distributed / Communication

- **Status:** Target-state scaffold; no production capability is claimed by this file.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

Collective operations and the gradient/metric reduction path: `collectives.py`, `comm_hooks.py`, `gradient_sync.py`, `loss_reduction.py`, `metric_reduction.py`, plus the `transport.py` seam and `diagnostics.py`. This package owns how ranks exchange tensors, not how they are arranged -- that is `topology/` -- and not which parallelism applies them, which is `parallelism/`.

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
