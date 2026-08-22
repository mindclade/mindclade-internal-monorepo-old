<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Models

> **Maturity:** Scaffolded overall, with implemented and locally tested
> PyTorch leaves; no production model capability is claimed here.
> **Primary implementation:** Python and PyTorch.

`models/` reserves the contracts and reusable implementation boundaries for
model families, components, adapters, references, and registry metadata.

## Implemented, unqualified leaves

- Dense multi-head scaled-dot-product attention, boolean allowed masks,
  rotary embeddings, and an injectable attention-operator boundary.
- RMSNorm, LayerNorm, SwiGLU, feed-forward, and residual components with
  explicit shape, dtype, and finite-parameter validation.
- A deterministic decoder-only LLM reference stack with pre-normalization,
  causal RoPE attention, SwiGLU blocks, tied embeddings, and stable outputs.
- A bounded `torch.export` package format with exact tensor contracts,
  content digests, non-overwriting atomic publication, trusted-digest loading,
  and fresh-module eager/export parity checks.

These are reusable reference and integration slices, not a claim that every
model family or adapter is implemented. Generation/cache semantics,
checkpoint migration, training systems, ONNX/AOTInductor export, distributed
execution, quantization, serving integration, and accelerator qualification
remain outside the implemented boundary.

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Model, configuration, state, checkpoint, export, provenance, and compatibility contracts |
| [`components/`](components/) | Reusable attention, embedding, geometry, loss, normalization, and neural-network components |
| [`families/`](families/) | Independent biology, diffusion, language, MoE, and multimodal families |
| [`adapters/`](adapters/) | Export, Hugging Face, and serving integration boundaries |
| [`reference/`](reference/) | Small reference models for deterministic testing and examples |
| [`registry/`](registry/) | Model catalog, resolution, factories, and validation |

## Boundary

- Model families do not import one another.
- Reusable model and numerical logic stays here; deployable process and network
  wiring belongs under [`services/`](../services/).
- Runtime request, response, and bundle formats belong under
  [`serving/contracts/`](../serving/contracts/).
- Cross-language data uses [`protocols/`](../protocols/) rather than Python
  implementation types.

## Start here

- Read [`contracts/README.md`](contracts/README.md) before changing a public
  model boundary.
- Use [`reference/README.md`](reference/README.md) for test-oriented reference
  models.
- Check the [model registry](registry/README.md) and
  [`models/registry.yaml`](registry.yaml) for catalog shape.

## Promotion bar

Promotion requires named ownership, stable contracts, bounded resource and
failure behavior, package-local tests, Bazel ownership, compatibility and
rollback policy, numerical evidence, and current qualification. Until then,
[`components.toml`](../components.toml) remains authoritative.
