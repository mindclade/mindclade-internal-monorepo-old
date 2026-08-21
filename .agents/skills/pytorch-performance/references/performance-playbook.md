# Performance playbook

## Benchmark record

Capture:

- hardware model and device count;
- operating system, Python, PyTorch, CUDA or other runtime versions;
- model mode, batch and input shapes, dtype, and memory format;
- eager or compiled state and compile mode;
- warmup count and measured iterations;
- latency statistic and variability;
- peak allocated memory when relevant;
- correctness tolerance and reference path.

## Microbenchmarks

Use `torch.utils.benchmark.Timer` for focused operations. `blocked_autorange` and `adaptive_autorange` reduce timer overhead and provide repeated measurements. Keep setup outside the measured statement and include realistic shapes and strides.

## End-to-end profiling

Use a bounded `torch.profiler.profile` window. For long jobs, use a schedule with wait, warmup, and active phases, call `profiler.step()` once per logical step, and export a trace or inspect key averages. Profiling itself adds overhead, so do not report profiled iteration time as the final benchmark.

## Bottleneck clues

- Low accelerator utilization plus DataLoader gaps suggests input or host scheduling limits.
- Many tiny kernels or Python frames suggests launch or dispatch overhead and may benefit from batching, vectorization, or compilation.
- Frequent device synchronizations often come from scalar extraction, host copies, logging, or explicit synchronization.
- High memory traffic with low arithmetic intensity may need layout, fusion, or algorithm changes rather than more compute.
- Repeated compilation for new shapes suggests guards or dynamic-shape behavior need attention.
- Large communication spans in distributed traces require overlap, bucket, sharding, or topology analysis, not local kernel tuning alone.

## torch.compile

Start with a stable eager reference. On current PyTorch versions, applying `model.compile()` to a top-level module can be preferable to replacing the module object with `torch.compile(model)`. Distributed placement is version- and wrapper-specific: some DDP guidance wraps before compilation so DDPOptimizer can use bucket boundaries, while compiler troubleshooting may recommend compiling the inner module when wrapper compilation is problematic. Check the installed-version documentation, test the supported alternatives, and record the chosen placement. Do not infer FSDP behavior from DDP guidance.

Track:

- first-run compile latency;
- steady-state latency;
- graph breaks;
- recompilations across input shapes;
- eager-versus-compiled numerical parity;
- memory change;
- fallback behavior.

## Mixed precision

Use autocast rather than manually converting the whole model. Choose float16 or bfloat16 according to backend support and numerical behavior. Validate outputs, losses, gradients, and convergence-critical metrics.

## Memory optimization order

1. remove retained tensors and accidental graph lifetime;
2. verify batch and sequence sizes;
3. use appropriate precision;
4. reduce activation lifetime or use checkpointing with a measured compute tradeoff;
5. change optimizer or sharding strategy when state memory dominates;
6. consider architectural changes only with explicit semantic approval.

Official references:

- Profiler: https://docs.pytorch.org/docs/stable/profiler.html
- Benchmark utilities: https://docs.pytorch.org/docs/stable/benchmark_utils.html
- torch.compile: https://docs.pytorch.org/docs/stable/generated/torch.compile.html
- Compiler profiling: https://docs.pytorch.org/docs/stable/user_guide/torch_compiler/torch.compiler_profiling_torch_compile.html
- AMP: https://docs.pytorch.org/docs/stable/amp.html
