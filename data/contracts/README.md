# Data / Contracts

- **Status:** Provider-neutral contract core implemented; connected qualification pending.
- **Primary implementation ownership:** Python scientific semantics with Rust byte workers and Go workflow coordination

## Purpose

Reusable data contracts, connectors, curation, dataset publication, quality, tokenization, and loading semantics. Durable source/workflow state stays in Go; high-throughput transfer/parsing stays in Rust. This path specializes that domain for **contracts**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented behavior

The package defines immutable source snapshots, bounded field and dataset contracts,
content-addressed shards, reproducible dataset snapshots, and deterministic record validation.
Validation never coerces, drops, quarantines, logs, or publishes data; it returns stable issues
to the owning pipeline. Proprietary and restricted fields cannot opt into verbatim logging, and
source URIs cannot carry query credentials.

The implementation deliberately does not fetch data, persist manifests, decide lawful use,
perform deletion, or promote a dataset. Provider adapters and connected qualification must prove
those behaviors before any consuming pipeline advances beyond its own declared maturity.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
