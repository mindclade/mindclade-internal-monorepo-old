# Training / Distributed / Planning

- **Status:** Target-state scaffold; no production capability is claimed by this file.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

Compiling a declared parallelism into an executable plan: `plan.py`, `compiler.py`, `fingerprint.py`, `collective_schedule.py`, `transformation_order.py` and `compatibility.py`. The per-architecture plans live in `plans/`. The fingerprint is the checkpoint-compatibility identity, so topology changes are detectable across a resume.

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
