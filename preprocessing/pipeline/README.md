# Preprocessing / Pipeline

- **Status:** Implemented provider-independent planning and resume core; not production-qualified.
- **Primary implementation ownership:** Python scientific semantics with Rust-supervised external tools

## Purpose

Reusable scientific preprocessing for entities, MSAs, templates, ligands, chemistry, multimodal inputs, caching, provenance, and feature bundles. This path specializes that domain for **pipeline**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented boundary

The package validates bounded job prefixes, canonical configuration/reference digests, and a
deterministic stage DAG. Resume accepts only dependency-closed completion sets whose recorded
descriptor digest exactly matches the current plan, preventing stale configuration or reference
work from being reused under a stable stage id.

## Remaining qualification boundary

Production use still requires:

- a durable completion repository with fencing/lease semantics;
- output artifact existence and integrity verification against the artifact provider;
- cancellation, retry, and concurrent-resumer integration tests;
- connected performance, failure-injection, and rollback evidence for the selected executor.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
