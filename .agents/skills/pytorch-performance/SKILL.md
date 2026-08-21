---
name: pytorch-performance
description: Benchmark, profile, and optimize PyTorch training or inference for latency, throughput, memory, utilization, data loading, mixed precision, torch.compile, graph breaks, and kernel behavior. Use when performance is a stated problem and a comparable baseline can be measured. Do not use to change model semantics or claim speedups without measurements.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Improve a named performance metric while preserving correctness, using reproducible measurements and evidence from profiling rather than intuition.

# Workflow

1. Define the target metric and constraints: latency percentile, throughput, step time, tokens per second, peak memory, startup time, compile overhead, or cost. Record batch shape, dtype, device, hardware, PyTorch version, and acceptable numerical tolerance.
2. Create a correctness oracle and a benchmark that represents the real workload. Separate initialization, data loading, compilation, warmup, and steady-state execution.
3. Measure an eager baseline. Use repeated trials, warmups, accelerator synchronization where required, and robust statistics. For microbenchmarks, prefer `torch.utils.benchmark` over ad hoc wall-clock loops.
4. Profile a bounded representative window with `torch.profiler`. Include CPU and the relevant accelerator, use a wait-warmup-active schedule for long loops, and label high-level regions when useful.
5. Determine whether the dominant limit is input, Python overhead, host-device synchronization, memory bandwidth, kernel launch overhead, compute, communication, graph breaks, or excessive recompilation.
6. Apply one justified change at a time. Candidate changes include batching, vectorization, data-loader tuning, removing accidental synchronizations, AMP, memory format, fused public APIs, activation checkpointing, or compilation.
7. Add `torch.compile` only after eager correctness is stable. Compile the highest useful function or module that does not create excessive graph breaks. For distributed wrappers, consult the installed-version guidance and measure the supported placement: DDP documentation may favor wrapping before compilation to enable DDPOptimizer, while compiler troubleshooting may favor compiling the inner module when wrapper compilation is unstable. Do not assume one order applies to both DDP and FSDP.
8. Separate compile and warmup cost from steady-state gain. Test every representative shape, including dynamic boundaries, and inspect recompilations or graph breaks when gains are unstable.
9. Re-run correctness tests and the exact baseline protocol. Report median and variability, memory, warmup policy, number of trials, and both absolute and relative change.
10. Keep the smallest clear implementation. Revert complexity that does not produce a meaningful, repeatable gain.

# Measurement rules

- Never time asynchronous accelerator work without a synchronization-aware method.
- Do not include one-time model construction, data download, or compilation in a steady-state metric unless startup latency is the target.
- Do not compare different batch shapes, dtypes, devices, data paths, or correctness tolerances without calling out the difference.
- Treat reduced precision as a numerical change that requires parity checks, not only a speed flag.
- Avoid repeated `.item()`, host copies, printing, or logging in the measured inner loop.
- Use profiler traces to choose targets, but use a lower-overhead benchmark to report final performance.
- Do not use private PyTorch internals when a stable public API solves the problem, unless the repository explicitly accepts the maintenance cost.

# Definition of done

- The target metric and benchmark protocol are documented.
- Eager correctness is protected by tests.
- A profiler or equivalent evidence identifies the bottleneck.
- The optimized path is compared under the same workload and tolerance.
- Compile overhead, warmup, memory, and variability are reported separately when relevant.
- The result is either a demonstrated improvement or an evidence-backed conclusion that the attempted change does not help.

Read [the performance playbook](references/performance-playbook.md) for benchmark and compiler guidance.
