<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Configuration catalog

> **Maturity:** Target-state catalog with mixed implementation and qualification.
> **Primary implementation:** Language-neutral TOML and JSON Schema.

`configs/` holds composable, reviewable configuration inputs. A run resolves
these inputs into one canonical immutable document with recorded source
precedence and a deterministic digest.

## What's here

| Path | Responsibility |
| --- | --- |
| [`base/`](base/) | Baseline ingestion, preprocessing, training, evaluation, and inference settings |
| [`environments/`](environments/) and [`profiles/`](profiles/) | Environment- and workload-specific overlays |
| [`recipes/`](recipes/) | Reviewed reusable configuration compositions |
| [`evaluation/`](evaluation/) | Presubmit, nightly, release, safety, and biology evaluation inputs |
| [`qualification/`](qualification/) | Failure-injection and performance qualification inputs |
| [`release/`](release/) | Bundle, signing, qualification, and rollback inputs |
| [`schemas/`](schemas/) | JSON Schemas for run, dataset, serving, and release documents |

## Boundary

- This directory owns declarative inputs and schemas, not resolution behavior.
- Deterministic resolution lives in
  [`libs/python/config/`](../libs/python/config/).
- Secrets never belong in checked-in configuration; services obtain them
  through approved provider boundaries.
- Deployment-specific values stay with the owning environment under
  [`infra/`](../infra/) or a protected external configuration plane.

## Start here

- Read the [resolved configuration guide](../docs/guides/config-resolution.md).
- Use [`schemas/run.schema.json`](schemas/run.schema.json) for the canonical run
  document shape.
- Check [`maturity.toml`](../maturity.toml) and
  [`SCAFFOLD_STATUS.md`](../SCAFFOLD_STATUS.md) before treating a profile as
  qualified.

## Promotion bar

A configuration becomes supported only with schema validation, deterministic
resolution, compatibility rules, tests, ownership, and qualification evidence
for every environment in which it is used.
