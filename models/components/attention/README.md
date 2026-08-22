# Models / Components / Attention

**Status:** Dense PyTorch component implemented and package-tested on CPU; sparse
attention remains scaffolded and accelerator qualification remains separate.

## Tensor contract

`DenseMultiheadAttention` accepts query `[B,Tq,C]`, key/value `[B,Tk,C]`, and
returns `[B,Tq,C]` on the same device and with the same floating dtype. `C` must
equal `embed_dim`; key and value sequence lengths must match. Empty sequences,
mixed devices/dtypes, and implicit channel broadcasting are rejected.

Masks are boolean with `True` meaning allowed. Accepted layouts are `[B,Tk]`,
`[B,Tq,Tk]`, and `[B,1|H,Tq,Tk]`. Explicit masks may be combined with causal
attention. A query row with no allowed keys returns an exact zero after the
output projection, so projection bias cannot turn a padded row into data.
Finite inputs use PyTorch's stable SDPA implementation and produce finite values
for fully masked rows; NaN or infinity in caller inputs is outside this contract.

`RotaryEmbedding` accepts `[...,T,D]`, uses interleaved even/odd channel pairs,
and optionally accepts integer positions `[T]` on the same device. `D` must be
the configured positive even head dimension.

## Operator boundary

`AttentionOperator` is an injected, registered child-module seam over projected
`[B,H,T,D]` tensors. `PyTorchSDPAOperator` is the only default. TileLang or vendor
operators must be supplied by a caller after their exact semantic and runtime
domain is qualified; importing this package never enables one.

Dropout follows normal module state: configured dropout is active only in
training mode and is zero in evaluation. Parameters and rotary frequencies use
ordinary `state_dict` serialization.

## Remaining work

`sparse.py` remains an explicit scaffold. CUDA/reduced-precision parity,
performance, compilation, and deployment evidence are also outside the local CPU
qualification represented by the package tests.
