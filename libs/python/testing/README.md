# Python test primitives

This package contains deterministic helpers used across Python test suites: bounded numerical and
rotation assertions, pure device-spec selection, multi-rank environment matrices, an environment
fixture that restores the exact prior snapshot, and shell-free subprocess execution.

Numerical assertions accept at most 4,000,000 elements and report shape, first mismatch, mismatch
count, and maximum finite absolute error. `run_process` executes an argument vector directly with
stdin disabled, enforces a deadline of at most 300 seconds, and terminates a process once aggregate
stdout/stderr exceeds 1 MiB. It never interpolates a command through a shell. Device selection is
based only on an explicit inventory and never probes or initializes an accelerator runtime.

These helpers do not manage production processes, initialize distributed backends, reserve ports,
discover GPUs, or provide application fixtures. `temporary_environ` mutates process-global state
and therefore must not be used concurrently within one interpreter.
