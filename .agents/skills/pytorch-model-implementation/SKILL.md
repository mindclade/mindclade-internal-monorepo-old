---
name: pytorch-model-implementation
description: Implement or refactor PyTorch nn.Module code with correct tensor shapes, devices, dtypes, parameter and buffer registration, autograd behavior, and state_dict compatibility. Use for model architectures, layers, losses, forward methods, custom autograd, or model-code defects. Do not use as the main workflow for full training loops or benchmarking.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Produce model code whose tensor contract is explicit, whose parameters and buffers serialize correctly, and whose forward and backward behavior is verified on representative inputs.

# Workflow

1. Read repository instructions, the model call sites, data batch structure, loss, checkpoint code, and tests before changing the module.
2. Detect the installed PyTorch version and the devices and dtypes the repository supports. Preserve public APIs and checkpoint key names unless a migration is part of the request.
3. Write down the input and output contract before implementation: nested structure, shape symbols, dtype, device, padding or masking semantics, and train-versus-eval behavior.
4. Implement submodules in `__init__` so they are registered. Use `register_buffer` for persistent or non-persistent tensor state that should follow device moves but is not optimized.
5. Create new tensors from existing tensors when possible with `new_*`, `zeros_like`, or explicit `device` and `dtype`. Do not hide device transfers inside the model unless the repository contract requires it.
6. Preserve batch dimensions and name every `squeeze`, `unsqueeze`, `view`, `reshape`, `transpose`, or `permute` assumption. Prefer shape assertions near boundaries over mysterious downstream failures.
7. Avoid in-place mutation of values autograd may need. For custom gradients, use public `torch.autograd.Function` patterns and add `gradcheck` coverage.
8. Verify initialization and parameter sharing intentionally. Confirm tensors intended as parameters appear in `named_parameters`, and tensors intended as buffers appear in `named_buffers`.
9. Run a tiny forward pass, a scalar loss, backward, finite-gradient checks, train and eval behavior checks, and a `state_dict` round trip.
10. Report the tensor contract, parameter-count or checkpoint-key changes, tests run, and compatibility implications.

# Engineering rules

- Never use plain Python lists or dictionaries for learnable submodules; use `ModuleList`, `ModuleDict`, `ParameterList`, or `ParameterDict` where registration is required.
- Avoid bare `squeeze()` when a batch dimension can equal one. Pass the exact dimension.
- Do not call `.cuda()` or choose a global accelerator in module construction. Let the caller place the module.
- Do not silently cast all inputs to float. Preserve integer token or index tensors and make dtype conversion part of the documented contract.
- Keep training-only stochastic behavior behind module training state and verify `model.train()` and `model.eval()` semantics.
- Prefer `state_dict` compatibility. When renaming keys is unavoidable, provide an explicit migration and test it.
- Do not add `torch.compile` solely while implementing correctness. Establish an eager reference first.

# Definition of done

- Representative valid inputs produce outputs with documented structure, shape, dtype, and device.
- Invalid inputs fail near the boundary with an actionable message when validation is appropriate.
- A backward pass reaches every parameter expected to learn, with finite gradients.
- Evaluation mode is deterministic to the degree promised by the module.
- Saving and loading a `state_dict` preserves outputs within explicit tolerances.

Read [the model contracts reference](references/model-contracts.md) before implementing shape-sensitive or checkpoint-sensitive modules.
