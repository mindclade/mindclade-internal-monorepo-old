# Serving architecture

Serving has two distinct paths that share model-worker contracts but different
latency and durability requirements.

## Online prepared-input inference

```text
client
  -> Rust runtime gateway
  -> Rust runtime host
  -> Python/PyTorch model worker
  -> qualified TileLang kernels
  -> streamed response and artifacts
```

The input is already model-ready or requires only bounded synchronous
validation. Rust owns network admission and streaming; Python owns final tensor
batching and execution.

## Durable full-pipeline prediction

```text
client submits run to Go control plane
  -> ingestion/preprocessing stages
  -> immutable PreprocessedInputBundle
  -> scheduled GPU inference
  -> confidence/ranking/evaluation
  -> terminal artifacts and run state
```

This path covers NovaFold-style MSA/template work and returns a durable `run_id`.
SSE may stream status, but the workflow does not rely on one long-held request.

## Batching

Rust performs coarse grouping and reserves resources using signed model
manifests. Python validates exact tensor compatibility and forms the actual
batch based on tokens, atoms, chains, MSA depth, templates, seeds, diffusion
samples, recycles, cache layout, compilation buckets, and device memory.

## Model worker contract

A worker loads an immutable model/runtime bundle, validates input schema,
reports capabilities and memory estimates, accepts batch plans, executes,
emits progress/status sequence numbers, stages artifacts, and supports bounded
cancel/drain. It does not own global routing or durable job state.

## Safety and audit

The Go control plane owns durable policy and audit. Rust enforces locally signed
claims and request bounds. Python performs model-specific input/output safety
checks where scientific semantics are required. Every result records model,
runtime, route, policy, kernel-provider, input-bundle, and environment digests.
