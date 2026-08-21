# Kernel qualification

- **Status:** Implemented evidence and promotion model; zero TileLang signatures are promoted by this source change.
- **Authority:** [ADR-0008](../../docs/design/adr-0008-qualified-tilelang-kernels.md)

Qualification is exact and content-addressed. Evidence binds the request and
implementation digests, repository revision, generated-source digest,
environment digest, numerical results, performance distribution, and soak
result. Failed evidence remains serializable for audit.

`PromotionPolicy` rejects records unless forward parity, required gradients,
determinism, sanitizers, minimum case counts, stability, compile behavior, and
minimum baseline speedup pass. `qualification_candidate` is pure: it returns a
candidate record but never writes a manifest or changes production state.

Revocations are separate immutable records. Dispatch checks revocation before
selection and falls back to PyTorch if the exact qualification is absent or
revoked. The environment kill switch supplies an independent emergency rollback.

## Evidence sequence

1. Validate the semantic contract and eligibility boundary.
2. Compile the exact source/schedule/toolchain and inspect generated code.
3. Run adversarial forward, gradient, determinism, and sanitizer coverage.
4. Benchmark against the named baseline with synchronized samples.
5. Soak the exact target and review the machine-readable evidence.
6. Publish through the separately authorized promotion workflow.

This repository change implements steps 1–5 tooling only. It does not authorize
step 6 and makes no production performance claim.
