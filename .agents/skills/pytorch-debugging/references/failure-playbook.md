# PyTorch failure playbook

## Shape and layout

Inspect shape, stride, contiguity, named dimensions, reduction axes, and broadcast pairs. Test batch size one and the smallest valid dimensions. Replace a bare `squeeze()` with an explicit axis only when that is the real contract.

## Device

Trace where the model, inputs, targets, masks, constants, optimizer state, and newly created tensors are placed. Avoid hidden transfers. For distributed code, verify local-rank placement before wrapping the model.

## Dtype

Check integer indices, boolean masks, floating inputs, parameter dtypes, autocast regions, reduction dtypes, and loss expectations. A dtype mismatch can be caused by a single constant or data transform.

## Missing gradients

Look for:

- `detach`, `.item()`, NumPy conversion, or tensor reconstruction;
- computation under `no_grad` or `inference_mode`;
- non-differentiable indexing or selection used incorrectly;
- parameters omitted from the optimizer;
- overwritten or unregistered parameters;
- a loss independent of the expected parameter;
- in-place mutation or custom autograd defects.

Use `gradcheck` for small double-precision custom operations.

## NaN and Inf

Check inputs first, then forward boundaries, individual loss terms, gradients before and after unscale, optimizer state, and parameters after the step. Record the first step and first tensor that becomes non-finite. Typical causes include invalid logs or square roots, division by tiny values, overflow, unstable normalization, incorrect masking, and overly aggressive updates.

## Memory and OOM

Separate:

- persistent model, gradient, optimizer, and buffer memory;
- activation peak;
- temporary workspaces;
- retained computation graphs;
- DataLoader or host memory;
- compiler cache or shape recompilation effects.

Log allocated and peak memory around a short stable window. Confirm that metrics and debugging collections store scalars or detached CPU values rather than live graph tensors.

## DataLoader workers

Start with `num_workers=0`. Check pickling, main guards, worker-local resources, corrupt examples, shared-memory limits, and worker seed behavior. Restore workers one at a time.

## Compile or export

Compare eager execution first. Identify graph breaks, data-dependent Python, unsupported custom operations, dynamic shape assumptions, and recompilations. Keep a correctness test that compares eager and compiled or exported results across representative shapes.

## Distributed hangs

Check that every rank enters collectives in the same order and with compatible tensor metadata. Ensure rank-zero-only branches do not skip a collective. Reduce to two local processes and add rank-tagged logs around collective boundaries.

Official references:

- Autograd mechanics: https://docs.pytorch.org/docs/stable/notes/autograd.html
- Numerical accuracy: https://docs.pytorch.org/docs/stable/notes/numerical_accuracy.html
- CUDA semantics: https://docs.pytorch.org/docs/stable/notes/cuda.html
- Multiprocessing: https://docs.pytorch.org/docs/stable/notes/multiprocessing.html
- Compile troubleshooting: https://docs.pytorch.org/docs/stable/user_guide/torch_compiler/torch.compiler_troubleshooting.html
