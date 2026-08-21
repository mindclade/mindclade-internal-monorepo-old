# Data / Datasets / Manifests

- **Status:** Release-evidence templates; no dataset or reference release is represented here.
- **Primary implementation ownership:** Python scientific semantics with Rust byte workers and Go workflow coordination

## Purpose

Reusable data contracts, connectors, curation, dataset publication, quality, tokenization, and loading semantics. Durable source/workflow state stays in Go; high-throughput transfer/parsing stays in Rust. This path specializes that domain for **manifests**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Template use

Files in this directory are non-release examples. A real release must be
generated from the implemented typed manifest contracts, pin immutable artifacts
and digests, carry quality and lineage evidence, pass the Go publication state
machine, and be independently approved where policy requires it.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
