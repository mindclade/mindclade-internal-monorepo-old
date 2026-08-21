# Data / Connectors / Google Cloud Storage Adapter

- **Status:** Implemented source contract; connected production qualification is pending.
- **Primary implementation ownership:** Python scientific semantics with Rust byte workers and Go workflow coordination

## Purpose

Reusable data contracts, connectors, curation, dataset publication, quality, tokenization, and loading semantics. Durable source/workflow state stays in Go; high-throughput transfer/parsing stays in Rust. This path specializes that domain for **Google Cloud Storage adapter**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Qualification boundary

The source contract is implemented with package-local tests and Bazel ownership.
Production promotion still requires:

- connected provider and representative-volume evidence for enabled paths;
- reviewed workload identity, IAM, network, encryption, retention, and audit
  configuration;
- approved SLO, capacity, cost, failure-injection, rollback, restore, and DR
  evidence;
- signed release evidence for every promoted dataset or reference snapshot; and
- the remaining gates in [`data/PRODUCTION_READINESS.md`](../../PRODUCTION_READINESS.md).

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
