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

## Servable model descriptor

`mindclade.inference.v1.ModelDescriptor` is the published catalog state for one
servable model. It carries identity (model/engine/config/kernel/safety digests),
the capabilities the model declares, the coarse compatibility classes the data
plane may admit against, and the resource envelope the host reserves before a
batch is formed.

Ownership follows the language boundaries:

- **Go** (`control/registry/models`) is the only writer. It validates a
  descriptor, applies the publication policy, and seals `descriptor_digest`.
- **Rust** (`protocols/rust/src/inference/v1.rs`) admits a request into exactly
  one declared compatibility class and reserves against the envelope. It never
  interprets model internals; a rejection means "not admissible", never "this
  tensor layout is impossible".
- **Python** (`serving/contracts/model_descriptor.py`) recomputes the seal
  before serving, checks the loaded bundle is the one the descriptor names, and
  confirms a planned batch stays inside its declared class.

### Canonical encoding

`descriptor_digest` is the SHA-256 of the newline-framed
`inference-model-descriptor/v1` document. The format avoids protobuf and JSON
serializers so Go and Python agree byte for byte without sharing a library:

1. the literal document type `inference-model-descriptor/v1`;
2. one line each for model id, family, version, lifecycle, the five digests,
   accelerator capability, minimum runtime version, schema version, policy
   epoch, and the created/expires Unix-millisecond bounds;
3. one `capability|<value>` line per declared capability, sorted;
4. one `class|<class_id>|<execution_kind>|<precision>|<shape_bucket>|...` line
   per compatibility class, sorted by class id;
5. one `envelope|...` line.

Repeated fields are emitted sorted so the digest does not depend on the order a
descriptor was assembled in, and no field may contain a newline or a vertical
bar, which keeps the encoding injective. `tests/integration/cross_language`
holds the frozen vector; the Go writer generates it and the Python verifier
reproduces it independently.

### Limits and non-responsibilities

The descriptor does not describe padding, packing, KV/feature-cache layout,
CUDA-graph or compile-bucket selection, or diffusion scheduling — those stay
with Python. It does not carry routing weights, lease state, or signatures;
routing and execution authority live in `mindclade.runtime.v1`, and a descriptor
reaches the data plane inside an already-signed route snapshot. A consumer that
cannot recompute the digest must treat the descriptor as opaque and trust the
transport that carried it.

## Safety and audit

The Go control plane owns durable policy and audit. Rust enforces locally signed
claims and request bounds. Python performs model-specific input/output safety
checks where scientific semantics are required. Every result records model,
runtime, route, policy, kernel-provider, input-bundle, and environment digests.
