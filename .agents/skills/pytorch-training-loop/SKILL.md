---
name: pytorch-training-loop
description: Implement or repair PyTorch training and evaluation loops with correct mode switching, gradient accumulation, AMP, clipping, optimizer and scheduler ordering, metrics, checkpointing, resume, and reproducibility. Use for trainers, fine-tuning jobs, convergence plumbing, or checkpoint lifecycle work. Do not use as the main workflow for distributed sharding or isolated model-layer defects.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Build a training loop that is mathematically correct, resumable, observable, and testable before making it sophisticated.

# Workflow

1. Inspect the model, batch contract, objective, optimizer, scheduler, metrics, current checkpoint format, and distributed wrapper before editing the loop.
2. Define the unit of progress: microbatch, optimizer step, epoch, token, or sample. Use the same unit consistently for logging, scheduling, evaluation, and resume.
3. Implement a minimal eager float32 reference path first. Confirm it can overfit a tiny deterministic batch or dataset when the task permits.
4. Separate train and evaluation behavior. Use `model.train()` for training and `model.eval()` plus `torch.inference_mode()` for pure evaluation when compatible.
5. At each optimization boundary, clear gradients intentionally, run forward and loss, run backward, apply clipping if configured, step the optimizer, update mixed-precision scaling, and step the scheduler according to its documented semantics.
6. For gradient accumulation, normalize the loss or gradients consistently, step only at accumulation boundaries, handle the final partial group, and make logged loss denominators explicit.
7. Add AMP only after the reference path works. Use `torch.amp.autocast` for forward and loss. Use gradient scaling for float16 paths that need it, unscale before clipping or gradient inspection, and do not run backward inside autocast.
8. Compute metrics from detached values. Aggregate numerators and denominators rather than averaging already-averaged batch metrics when batch sizes vary.
9. Save resumable state: model, optimizer, scheduler when present, scaler when present, progress counters, configuration identity, and the random state needed by the reproducibility contract.
10. Verify checkpoint round-trip and interrupted-versus-uninterrupted equivalence over a small controlled run to the tolerance the environment supports.
11. Report optimizer-step ordering, accumulation semantics, AMP dtype and scaling policy, scheduler unit, checkpoint contents, and exact validation commands.

# Engineering rules

- Prefer `optimizer.zero_grad(set_to_none=True)` unless code depends on materialized zero tensors.
- Do not call `.item()` repeatedly inside the critical path when it creates accelerator synchronization; collect only what logging needs.
- For accumulation, unscale once before clipping and call optimizer and scaler updates only at the real optimizer step.
- Do not silently skip non-finite losses or gradients. Stop with enough context to reproduce, or apply an explicitly requested recovery policy with counters.
- Save `state_dict` data, not whole live module objects, unless a trusted legacy format is an explicit requirement.
- Load untrusted checkpoints conservatively and prefer `weights_only=True` where compatible.
- Keep evaluation sampling, dropout, normalization statistics, and gradient state correct.
- Do not change the loss reduction or metric denominator merely to make curves look smoother.

# Definition of done

- A tiny run performs forward, backward, and at least one optimizer update with finite values.
- The training path can overfit a tiny batch when that is a meaningful sanity test.
- Evaluation does not mutate model parameters or accumulate gradients.
- AMP and float32 paths agree within explicit tolerances on a small case.
- A checkpoint restores the intended training state and progress.
- Logs distinguish microbatches from optimizer steps and use correct denominators.

Read [the training-loop checklist](references/training-loop-checklist.md) for ordering and resume details.
