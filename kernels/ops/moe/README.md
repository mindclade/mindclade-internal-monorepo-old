# Mixture-of-experts operation contracts

- **Implemented:** stable top-k routing, deterministic capacity assignment, padded grouped GEMM, and weighted combine references.
- **TileLang candidate:** Qualification-gated expert-major padded grouped GEMM on CUDA targets.

Routing resolves equal scores by ascending expert index. Capacity overflow is
explicit and deterministic; unused padded token rows must be zero. Grouped GEMM
uses `tokens[E,capacity,K] @ weights[E,K,N]` and leaves routing, communication,
and unpermutation outside the measured primitive.

The contract validates expert counts, shapes, capacities, dtypes, and routing
indices. Distributed all-to-all and load-balancing policy remain model/runtime
responsibilities and are not implied by this kernel implementation.
