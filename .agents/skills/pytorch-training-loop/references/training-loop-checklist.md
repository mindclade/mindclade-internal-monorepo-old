# Training-loop checklist

## Canonical optimizer step

The exact code depends on the installed PyTorch version and AMP policy, but the ordering should remain explicit:

1. clear or retain gradients according to accumulation state;
2. run forward and loss inside autocast when enabled;
3. divide or otherwise normalize for accumulation;
4. backward, with scaling when required;
5. at an optimizer boundary, unscale before gradient clipping or inspection;
6. validate or clip gradients;
7. optimizer step;
8. scaler update when used;
9. scheduler step at its documented unit;
10. clear gradients for the next accumulation group.

## AMP policy

Current public APIs use `torch.amp.autocast(device_type=...)` and `torch.amp.GradScaler(...)`; older repository pins may require a compatibility path. CUDA float16 training commonly uses scaling. CPU bfloat16 autocast commonly does not. Choose dtype and scaling based on the actual backend and numerical behavior, not a blanket `half()` conversion.

Autocast should cover forward and loss. Backward outside the autocast context uses the dtypes selected during forward.

## Gradient accumulation

State explicitly:

- the number of microbatches per optimizer step;
- whether the loss is a mean or sum;
- how the final short accumulation group is normalized;
- whether scheduler and logging counters advance per microbatch or optimizer step;
- whether distributed gradient synchronization is suppressed for non-boundary microbatches.

## Metric aggregation

For an average loss, accumulate `loss_sum` and `example_count` or the precise reduction denominator. For accuracy, aggregate correct predictions and total predictions. In distributed runs, reduce these totals before division.

## Checkpoint contents

A resumable checkpoint often includes:

```text
model state_dict
optimizer state_dict
scheduler state_dict, if present
GradScaler state_dict, if present
epoch and optimizer-step counters
sampler or data position when exact mid-epoch resume is promised
Python, NumPy, CPU, and accelerator RNG states required by policy
configuration or run identifier
format version
```

Test loading into a fresh process or fresh objects, not only back into the same live objects.

## Sanity tests

- one CPU batch forward and backward;
- one optimizer step changes at least one expected parameter;
- all inspected gradients and losses are finite;
- tiny-batch overfit;
- train and eval behavior differ only where intended;
- save and resume matches an uninterrupted short run within tolerance;
- accumulation result matches a larger reference batch when the model and loss make that equivalence valid.

Official references:

- AMP: https://docs.pytorch.org/docs/stable/amp.html
- AMP examples: https://docs.pytorch.org/docs/stable/notes/amp_examples.html
- Reproducibility: https://docs.pytorch.org/docs/stable/notes/randomness.html
- Serialization: https://docs.pytorch.org/docs/stable/notes/serialization.html
