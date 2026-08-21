<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Data platform

> **Maturity:** Implemented source capability. Connected provider, distributed
> scale, security, SLO, and disaster-recovery qualification remains required
> before an environment may claim production readiness.
> **Primary implementation:** Python scientific semantics, Rust byte workers,
> and Go workflow coordination.

`data/` owns reusable data contracts, ingestion semantics, curation, dataset
publication, quality controls, tokenization, reference data, and loading.

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Source, record, shard, snapshot, and dataset contracts |
| [`connectors/`](connectors/) | External source adapters and bounded retrieval |
| [`ingestion/`](ingestion/) | Canonical ingestion stages, validation, and publication |
| [`curation/`](curation/) | Consent, licensing, filtering, deduplication, contamination, and provenance |
| [`datasets/`](datasets/) | Dataset catalogs, mixtures, versions, lineage, and manifests |
| [`quality/`](quality/) | Integrity, privacy, leakage, bias, safety, and license gates |
| [`reference/`](reference/) | Versioned reference database sources, snapshots, indexes, and manifests |
| [`tokenizers/`](tokenizers/) | Modality-specific tokenization and vocabulary contracts |
| [`loaders/`](loaders/) | Sampling, sharding, collation, prefetch, and worker behavior |

## Boundary

- Python owns scientific record and transformation semantics.
- Go owns durable source, workflow, lease, and publication state under
  [`control/ingestion/`](../control/ingestion/).
- Rust owns bounded transfer, parsing, cache, and worker execution in runtime
  services and libraries.
- Cross-language records and events use [`protocols/`](../protocols/); no
  language-private structure becomes an implicit wire contract.

## Start here

- [Data ingestion architecture](../docs/architecture/data-ingestion.md)
- [Dataset publication architecture](../docs/architecture/dataset-publication.md)
- [Reference data and release evidence](../docs/architecture/reference-data-and-release-evidence.md)
- [`data/curation/README.md`](curation/README.md) for the implemented curation
  pipeline
- [Data context](DATA_CONTEXT.md) for identities, metrics, filters, and semantic
  gotchas
- [Production readiness](PRODUCTION_READINESS.md) for the evidence-backed
  promotion decision and remaining environment gates

## Invariants

- Every artifact is addressed by immutable generation/version and SHA-256
  digest; mutable aliases are discovery inputs, never training identities.
- Dataset versions pin source snapshots, transforms, tokenizer/feature
  versions, split policy, lineage, quality evidence, and intended/prohibited
  uses.
- Quality output is aggregate-only. Raw records, sensitive identifiers, and
  credentials must not enter logs or reports.
- Split assignment is group-aware and performed before stochastic transforms.
- Go remains the durable workflow/publication authority. This package validates
  transitions and builds evidence but does not publish by itself.
- MLflow is an optional experiment/lineage mirror, never the canonical dataset
  registry or artifact store.

Always confirm component maturity in [`components.toml`](../components.toml) and
required evidence in [`QUALIFICATION.md`](../QUALIFICATION.md).
