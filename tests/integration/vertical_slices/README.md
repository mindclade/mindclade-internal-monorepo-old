# Golden vertical release slices

These tests qualify the architecture across language and durability boundaries. They are **platform qualification**, not a substitute for model-family numerical qualification.

The release gate executes four named slices:

1. **Data ingestion** — Go source/snapshot/workload policy, Python curation, Rust ingestion/artifact workers.
2. **NovaFold-style preprocessing** — entity/MSA/template/ligand/feature planning, immutable cache/provenance, Python final model-worker boundary, Rust node/artifact execution.
3. **Online inference** — signed policy boundary, Rust serving runtime/gateway/host, Python tensor-aware final batching.
4. **Training/release** — deterministic reference training state, checkpoint identity, evaluation identity, Go release-evidence DAG, Rust checkpoint/node path.

The deterministic reference training engine intentionally qualifies checkpoint/release plumbing only. It must never be presented as training-numerics or model-capability evidence.

`release_gate.py --require-rust` is the production release mode and fails closed if Cargo/Rust is unavailable.
