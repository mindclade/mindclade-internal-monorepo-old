# Kernel qualification tools

- **Status:** Implemented fail-closed tooling; no manifest is published by these commands.
- **Owner:** `biology-ml`

This package owns four Bazel entry points:

- `autotune_tilelang` accepts a bounded content-addressed candidate list and
  invokes a worker subprocess without a shell. It enforces a deadline and a
  strict, size-bounded JSON response, but does not provide operating-system or
  container isolation. Untrusted workers require an external sandbox. A
  candidate is timed only after the worker reports parity, and results are
  published atomically without overwriting an existing result path.
- `inspect_tilelang_ir` performs bounded lexical checks for required and
  forbidden architecture tokens after excluding C/C++-style comments, then
  emits its content digest. This is heuristic evidence, not proof that an
  instruction is emitted in final machine code or executes at runtime.
- `qualify_tilelang` evaluates reciprocal inference/training evidence and
  emits an unsigned schema-v2 candidate manifest without overwriting an
  existing path. It cannot publish or change runtime dispatch.
- `verify_tilelang_manifest` validates pair integrity, revocations, and
  a mandatory trusted manifest digest. Non-empty manifests additionally
  require exact environment, toolchain, and compiled-artifact identities.

All inputs are explicit JSON or captured source files. The autotune specification,
accepted worker streams, candidate counts, configuration sizes, sample counts,
sample values, and inspection source are bounded. Output files are machine-readable
and content-addressed. The subprocess inherits its caller's filesystem, network,
credentials, environment, and accelerator access unless the caller supplies an
external sandbox. Failures are terminal for the current candidate and never silently
create a qualification.

The connected invocation is selected by [`ci/gpu/targets.yaml`](../../../ci/gpu/targets.yaml).
Promotion remains a separate reviewed release authority.
