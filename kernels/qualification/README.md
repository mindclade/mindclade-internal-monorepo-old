# Kernel qualification

- **Status:** Implemented evidence and promotion model; zero TileLang signatures are promoted by this source change.
- **Authority:** [ADR-0008](../../docs/design/adr-0008-qualified-tilelang-kernels.md)

Qualification is exact and content-addressed. Evidence binds the request and
implementation digests, repository revision, generated-source digest,
environment digest, raw numerical and performance results, generated artifact,
attestation, and soak result. Failed evidence remains serializable for audit.

`PromotionPolicy` rejects records unless forward parity, required gradients,
determinism, sanitizers, minimum sample/process counts, median/MAD stability,
p95 latency, peak memory, compile behavior, and minimum baseline speedup pass.
`qualification_candidates` is pure and returns an inseparable reciprocal
inference/training pair; it never publishes a manifest or changes production
state. For inference-only TileLang implementations, the inference half executes
and benchmarks the candidate while the training half must prove PyTorch fallback
and reference gradients. The training half cannot claim candidate execution.

Revocations are separate immutable records. Dispatch checks revocation before
selection and falls back to PyTorch if the exact qualification is absent or
revoked. The environment kill switch supplies an independent emergency rollback.

## Evidence sequence

1. Validate the semantic contract and eligibility boundary.
2. Compile the exact source/schedule/toolchain and inspect generated code.
3. Run adversarial forward, gradient, determinism, and the complete sanitizer suite.
4. Benchmark against the named baseline with synchronized multi-process samples.
5. Soak the exact target and review the machine-readable evidence.
6. Publish through the separately authorized promotion workflow.

The reviewed matrix contains 124 inference/training pairs (248 exact requests)
across all seven operations. This repository implements steps 1–5 tooling only.
It does not authorize step 6 and makes no production performance claim.
