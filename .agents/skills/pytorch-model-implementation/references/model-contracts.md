# Model contract reference

## Contract template

Document each public forward input and output in this form:

```text
name: tokens
structure: Tensor
shape: [batch, sequence]
dtype: integer index type
device: same device as model
range: 0 <= tokens < vocabulary_size
padding: padding index is 0; mask is True for valid positions
```

For nested outputs, specify stable field names or tuple positions. Avoid returning a different structure based on training mode unless the existing API explicitly requires it.

## Registration checklist

- `nn.Module` children are assigned as attributes or stored in `ModuleList` or `ModuleDict`.
- Learnable tensors are `nn.Parameter` objects or stored in parameter containers.
- Non-learnable tensor state uses `register_buffer` when it must follow `.to(...)` or appear in the `state_dict`.
- Ephemeral caches are not persistent buffers unless checkpointing them is intentional.
- Shared parameters are deliberately shared and covered by a test.

## Shape safety

- Use named symbolic dimensions in comments and tests: `B`, `T`, `C`, `H`, `W`.
- Check batch size one, empty dimensions when supported, and the smallest valid spatial or sequence shape.
- Treat `view` as requiring compatible strides. Use `reshape` when a copy is acceptable or call `contiguous` for an explicit reason.
- Pass a dimension to `softmax`, `log_softmax`, reductions, `squeeze`, and concatenation.
- Confirm broadcasting is intentional; test a shape that would expose accidental broadcasting.

## Device and dtype safety

- Derive constants from an input or registered state when possible.
- Do not materialize CPU tensors in a CUDA or accelerator forward path accidentally.
- Keep index tensors integral.
- Be explicit about mixed-precision-sensitive reductions and normalizations.
- Test at least CPU float32. Test supported accelerator and reduced-precision paths conditionally.

## Autograd safety

- Avoid `.detach()`, `.item()`, NumPy conversion, or reconstruction with `torch.tensor(existing_tensor)` inside a differentiable path unless gradient breaking is intended.
- Avoid in-place writes to tensors needed for backward.
- For custom `autograd.Function`, test analytical versus numerical gradients with double precision and small inputs.
- Check that parameters expected to train have non-`None`, finite gradients after backward.

## State compatibility

Prefer saving and loading `state_dict` values rather than serialized module objects. A compatibility test should:

1. initialize a model;
2. run a reference input;
3. save the `state_dict`;
4. create a fresh model from configuration;
5. load the state strictly, unless an explicit migration is under test;
6. compare outputs with `torch.testing.assert_close` and explicit tolerances.

Official references:

- Modules: https://docs.pytorch.org/docs/stable/notes/modules.html
- Autograd mechanics: https://docs.pytorch.org/docs/stable/notes/autograd.html
- Extending autograd: https://docs.pytorch.org/docs/stable/notes/extending.html
- Serialization: https://docs.pytorch.org/docs/stable/notes/serialization.html
