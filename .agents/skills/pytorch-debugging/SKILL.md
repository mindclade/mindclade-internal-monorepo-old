---
name: pytorch-debugging
description: Diagnose and fix PyTorch failures involving tensor shapes, devices, dtypes, autograd, missing or exploding gradients, NaNs or Infs, CUDA or accelerator OOM, DataLoader workers, nondeterminism, compile errors, and incorrect checkpoint behavior. Use when a PyTorch program crashes, hangs, diverges, leaks memory, or produces wrong values. Do not use for speculative optimization without a defect.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Find the earliest incorrect boundary, create a minimal reproducer, fix the cause rather than the symptom, and add a regression test that would have caught it.

# Workflow

1. Capture the exact command, full traceback or failure symptom, seed, input shape, device, dtype, PyTorch version, and first known bad step. Run `scripts/torch_env_report.py` when environment details are incomplete.
2. Reproduce before editing. Reduce to one process, one batch, eager mode, float32, CPU, and `num_workers=0` as applicable, changing one dimension at a time until the failure boundary is isolated.
3. Classify the defect: shape or layout, device, dtype, autograd, numerics, memory lifetime, data pipeline, serialization, compile or export, distributed synchronization, or environment mismatch.
4. Instrument boundaries rather than every line. Log tensor structure, shape, dtype, device, stride, finiteness, value range, gradient presence, and memory counters where they can falsify a hypothesis.
5. Locate the first bad value or state, not merely the operation that finally throws. For NaNs, bisect forward activations, loss components, gradients, and optimizer state.
6. For autograd issues, inspect detach points, reconstruction, in-place mutation, no-grad contexts, custom functions, and whether the loss actually depends on the parameter.
7. For OOM or memory growth, distinguish peak working memory from retained tensors and fragmentation. Check accidental graph retention, lists of live tensors, unbounded caches, oversized batches, and optimizer state before applying memory workarounds.
8. For worker or hang issues, reproduce with zero workers and a tiny source, then restore multiprocessing and distributed pieces incrementally.
9. Implement the narrowest cause-level fix. Remove temporary anomaly detection and verbose hooks after the regression test is in place.
10. Run the minimal reproducer, relevant unit tests, and the original command or closest affordable case. Report the root cause, evidence, fix, and remaining uncertainty.

# Diagnostic rules

- Do not fix device errors by sprinkling `.cuda()` calls. Establish one placement contract.
- Do not fix dtype errors by converting every tensor to float. Preserve semantic integer and boolean data.
- Do not use `retain_graph=True` as a generic answer to repeated-backward errors. Determine why the graph is reused.
- Do not hide NaNs with `nan_to_num`, clipping, or skipped batches until the source and intended policy are known.
- Use autograd anomaly detection only as a temporary diagnostic because it is expensive and can change execution behavior.
- Do not call `empty_cache()` as a substitute for fixing live tensor retention; it does not free referenced tensors.
- Treat exact reproducibility across devices or PyTorch releases as a separate promise from repeatability within one environment.

# Definition of done

- A small reproducer fails before the fix and passes after it.
- The earliest incorrect tensor, state transition, or synchronization point is identified with evidence.
- The fix does not broaden casting, suppress exceptions, or silently discard data without policy.
- A regression test protects the corrected behavior.
- The original workload or a justified representative case is re-run.

Read [the failure playbook](references/failure-playbook.md) for category-specific checks.
