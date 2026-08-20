# Tools / Build / Bazel

- **Status:** Layer governance is materialized; platform/toolchain subpackages remain target-state scaffolds.
- **Primary implementation ownership:** Bazel/Nix/Python/Go/Rust development and qualification tooling

## Purpose

Repository-owned code generation, analysis, developer, qualification, and release tools. Tools are invoked through Bazel targets in production/CI paths. This path specializes that domain for **bazel**.

`layers.bzl` is active production policy. It owns the package groups declared by
the root BUILD and the forbidden-edge data consumed by
`tools/analysis/check_bazel_layers.py`. Its exception map accepts only exact
target edges backed by an ADR and is checked for stale entries.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Materialization requirements

Before the remaining scaffold modules in `extensions/`, `platforms/`, and
`rules/` are treated as implemented, add:

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
