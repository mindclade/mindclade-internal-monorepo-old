# Tools / Dev

- **Status:** Target-state scaffold; no production capability is claimed by this file.
- **Primary implementation ownership:** Bazel/Nix/Python/Go/Rust development and qualification tooling

## Purpose

Repository-owned code generation, analysis, developer, qualification, and release tools. Tools are invoked through Bazel targets in production/CI paths. This path specializes that domain for **dev**.

`git_hygiene_report.py` inventories local and `origin` branches, registered worktrees, unique
commits relative to `origin/main`, and dirty file paths before any manual cleanup. It is strictly
report-only: it has no delete, prune, reset, checkout, branch-update, or worktree-removal mode.
The scheduled workflow publishes JSON and static HTML from a clean hosted checkout; developers
must run it locally to include machine-local dirty or locked worktrees.

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
