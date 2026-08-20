<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Scientific preprocessing

> **Maturity:** Mixed; core contracts, DAG, and provenance seams are
> substantive, while provider and scale qualification is component-specific.
> **Primary implementation:** Python scientific semantics with Rust-supervised
> external tools.

`preprocessing/` turns validated source entities into deterministic,
provenance-rich feature bundles for model consumption.

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Entities, stages, search results, pipelines, and feature bundles |
| [`pipeline/`](pipeline/) | Planning, compilation, execution, resume, and validation |
| [`biology/`](biology/) | Entity featurization, MSAs, templates, and ligands |
| [`chemistry/`](chemistry/) | Canonicalization, conformers, descriptors, graphs, and validation |
| [`multimodal/`](multimodal/) | Alignment, layout, packing, and multimodal validation |
| [`cache/`](cache/) | Content-keyed lookup, storage, policy, and promotion |
| [`provenance/`](provenance/) | Database snapshots, toolchains, searches, and manifests |
| [`cli/`](cli/) | Inspection, preparation, MSA search, and template search entry points |

## Boundary

- Python owns scientific transformations and validation semantics.
- Rust supervises bounded external-tool execution; tools do not become an
  implicit source of workflow authority.
- Durable workflow state belongs in Go under
  [`control/ingestion/`](../control/ingestion/).
- Inputs, outputs, database snapshots, tool versions, and cache keys must be
  explicit enough to reproduce or reject a result.

## Start here

- [Preprocessing architecture](../docs/architecture/preprocessing.md)
- [MSA and template-search architecture](../docs/architecture/msa-and-template-search.md)
- [`contracts/README.md`](contracts/README.md) for stable package boundaries
- [`pipeline/README.md`](pipeline/README.md) for DAG behavior

Use [`preprocessing/tests/test_core_contracts.py`](tests/test_core_contracts.py)
for the current provider-independent contract evidence and
[`VALIDATION.md`](../VALIDATION.md) for remaining connected checks.
