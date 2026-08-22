# Decoder-only language model

- **Status:** bounded implementation slice; not promoted or pretrained
- **Owner:** Python/PyTorch model domain

This package implements a small, composable decoder-only transformer using pre-RMSNorm,
rotary positional embeddings, causal multi-head attention, and SwiGLU feed-forward blocks.
It is suitable for correctness tests, integration work, and as the eager semantic model for
an injected attention operator. Repository-level maturity remains governed by the component
manifest and qualification evidence.

## Public contract

`DecoderOnlyLanguageModel(config)` accepts:

- `input_ids`: dense strided `torch.int32` or `torch.int64`, shape `[B, T]`, on the model
  device, with every value in `[0, vocab_size)`;
- `attention_mask`: optional dense boolean `[B, T]` on the same device. `True` means the
  token is valid. Invalid query positions produce exact-zero hidden states and logits;
- shapes satisfying the immutable configuration's batch, sequence, token-count, and
  `B * num_heads * T * T` attention-element limits. Oversized requests fail; they are never
  truncated.

The stable `CausalLMOutput` contains `logits[B, T, vocab_size]` and final normalized
`hidden_states[B, T, hidden_size]`. Floating dtype follows model parameters; callers own dtype
and device placement. The model never moves or casts an input implicitly.

`attention_operator_factory` may create one fresh `AttentionOperator` per layer. RoPE is
applied to the projected queries and keys before the injected operator runs, so optimized
providers retain the eager attention semantics. Boolean pair masks use `True` for allowed
pairs through the shared attention contract.

## State and execution behavior

- Initialization is deterministic for a fixed `initialization_seed`: matrix parameters use
  a local CPU generator, biases are zero, normalization scales are one, and construction
  preserves the caller's process-global CPU RNG state.
- `tie_word_embeddings=True` deliberately aliases the token-embedding and bias-free LM-head
  parameter. Setting it to `False` allocates independent storage.
- Model dropout and attention dropout are stochastic only in `train()` mode. `eval()` mode is
  deterministic for fixed inputs on a deterministic backend.
- `state_dict` contains all parameters and persistent rotary inverse-frequency buffers and is
  strict-load compatible with a fresh object built from the same configuration.
- Tests cover static-shape `torch.export` save/load parity for the PyTorch exported-program
  runtime. Dynamic-shape, AOTInductor, ONNX, TensorRT, and accelerator-runtime parity are not
  claimed by this slice.

## Explicit non-responsibilities

This package does not implement or claim KV caching, generation/sampling, loss computation,
checkpoint file formats, pretrained weights, distributed execution, activation checkpointing,
quantization, or latency/throughput qualification. The existing files for those concerns remain
scaffolds until their own contracts and evidence are implemented.

Focused validation:

```bash
uv run pytest models/families/llm/tests/test_llm.py
uv run ruff check models/families/llm
```
