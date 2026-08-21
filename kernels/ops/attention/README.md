# Attention operation contract

- **Implemented:** `attention_reference`, `scaled_dot_product_attention`, and a qualification-gated TileLang online-softmax candidate.
- **Layout:** contiguous `Q[B,H,Sq,D]`, `K/V[B,H,Sk,D]`.

Boolean masks use `True = allowed` and broadcast to `[B,H,Sq,Sk]`. Causal
semantics allow keys with `key_index <= query_index`. Masking is applied before
softmax; fully masked rows produce zero output. FP16/BF16 inputs reduce in FP32,
while FP64 is retained for gradient checking.

The TileLang candidate does not materialize `[Sq,Sk]` in global memory. It tiles
Q/K/V in shared memory and maintains online row max/sum with FP32 accumulators.
Current accelerator registration covers dense non-causal dispatch; additional
mask and causal signatures require their own identities and qualification.
