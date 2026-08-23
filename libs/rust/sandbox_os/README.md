# `mindclade_sandbox_os`

Kernel-enforced syscall confinement for processes that handle untrusted input.
An allowlist type, a policy builder, reusable allowlist sets, and one explicit
`install()`.

Everything else in this tree that resembles isolation is cooperative — process
groups and bounded termination (`process_os`), sealed memfd segments (`ipc_os`),
userspace budget accounting (`runtime_core::budget`) — and all of it assumes the
confined code behaves. This does not. It hands the kernel a seccomp-BPF
allowlist and the kernel refuses everything else.

It sits *beneath* input validation rather than beside it. `bounded_parse` and
`bio_formats` decide what a FASTA or A3M file may contain; this decides what the
process can still do on the day one of those bounds is wrong.

## Fail closed

`install` returns `Confinement::NotRequested` when nothing was asked for,
`Confinement::Enforced` when a filter is in force, and an error otherwise. There
is no log-and-continue path, and `ViolationAction` has no survivable variant: a
filter whose violation path is an errno is an advisory, and the containment
layer beneath validation is not advisory.

## Not another unsafe exception

The crate keeps the workspace `unsafe_code = "deny"`. The `seccomp(2)` and
`prctl(2)` calls live behind `seccompiler`'s safe API, on the far side of the
dependency edge, so this is not a third entry in `UNSAFE_POLICY.md` alongside
`ipc_os` and `process_os`. ADR-0026 records that evaluation, including why
`libseccomp` bindings and Cloudflare's `foundations` were not used.

## Portability

seccomp-BPF is Linux-only. The policy types compile on every supported host so a
composition root reads identically everywhere; only the backend is
`#[cfg(target_os = "linux")]`, matching `ipc_os` and `process_os`. On
aarch64-darwin a required policy is an `Unimplemented` fault, never a silent
no-op.

Syscall *names* are the portable identity; numbers are resolved from a closed
table inside the Linux backend. A name the table does not know is a startup
failure, not a rule that quietly vanishes from the filter.

## Where it is installed

`worker_runtime::WorkerRuntime::start_confined` — between `Starting` and
`Ready`, before any ticket is leased and therefore before any untrusted input is
opened. A worker that asked for confinement and did not get it goes to `Failed`.
Parser crates never install anything: seccomp is process-scoped, and they do not
own the process.
