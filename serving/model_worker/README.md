# Serving / Model Worker

**Status:** implemented provider-neutral model runtime; connected model/hardware qualification pending.

The model worker owns final tensor-compatible batching, immutable model loading bounded by a
least-recently-used residency ceiling (`WorkerLimits.maximum_loaded_models`), explicit lifecycle
and drain behavior, process-local admission, and exact response cardinality. Advanced support
modules provide bounded contracts for continuous batching, memory/KV reservations, shape buckets,
precision selection, statistical seeds, generation/diffusion results, multimodal and biology
dimensions, compilation identity, warmup, and low-cardinality telemetry.

These modules deliberately do not invent numerical implementations. PyTorch models, tokenizers,
diffusion solvers, compiler backends, biology semantics, and accelerator kernels remain injected
from their owning packages and require parity, statistical, hardware, and performance evidence.
Rust owns signed admission, fencing, worker-process supervision, bulk buffers, and coarse routing;
Python remains the final authority for model-specific tensor compatibility.
