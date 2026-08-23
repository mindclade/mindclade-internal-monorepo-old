# ADR-0027: Kernel-enforced syscall confinement for untrusted-input processes

- **Status:** Accepted
- **Date:** 2026-08-23

## Decision

Rust processes that handle untrusted input install a seccomp-BPF allowlist at
process entry. The mechanism lives in `libs/rust/sandbox_os` (layer 0) and is
compiled by [`seccompiler`](https://crates.io/crates/seccompiler) `=0.5.0`,
pinned in the root `[workspace.dependencies]` with `default-features = false`.

Installation is a single explicit call, `mindclade_sandbox_os::install`, made
from a composition root. It is **fail closed**: it returns
`Confinement::NotRequested` when confinement was not asked for,
`Confinement::Enforced` when the kernel is enforcing a filter, and an error in
every other case. There is no path on which a required policy is unavailable and
the process continues. `ViolationAction` offers only terminating actions
(`KillProcess`, `Trap`); a filter whose violation path returns an errno is an
advisory, and this layer is not advisory.

`worker_runtime::WorkerRuntime::start_confined` is the first application: the
filter is installed between `Starting` and `Ready`, so it is in force before any
ticket is leased and therefore before any untrusted model input or scientific
file is opened. A worker that required confinement and did not get it
transitions to `Failed`, not `Ready`.

seccomp-BPF is Linux-only. The policy types compile on every declared host so a
composition root reads identically everywhere; only the backend is
`#[cfg(target_os = "linux")]`, following `ipc_os` and `process_os`. On
aarch64-darwin a required policy is a typed `Unimplemented` fault.

Cloudflare's [`foundations`](https://crates.io/crates/foundations) was evaluated
and **not adopted**. Its `security` API shape is modelled on; its `telemetry` and
`settings` features are declined. Reasons below.

## Rationale

### Why any of this

Before this change the repository had no kernel-enforced confinement of any
kind. A repository-wide search across `libs/rust`, `services`, `serving`,
`protocols` and `tools` for `seccomp|landlock|sandbox|prctl|setuid|setgid|
no_new_privs|unshare|cgroup` returned one hit: a comment about the macOS sandbox
in `services/runtime_host/src/async_ipc.rs`. Isolation was process groups and
bounded termination (`process_os`), sealed memfd segments (`ipc_os`), and
userspace budget accounting (`runtime_core::budget`). Every one of those assumes
the confined code behaves.

CLAUDE.md requires that all external biological files and model inputs be
treated as untrusted, and the parser work landing alongside this change is the
argument for a layer beneath validation: four of eight `bio_formats` parsers —
FASTA, A3M, FASTQ and Stockholm, the four most likely to arrive from a public
database — bypassed every bound through a fail-open match arm, and `record_io`
trusted a declared item count for pre-allocation at roughly 10^6:1 amplification.
Those are now fixed. The point of seccomp is the next one, which is not fixed
because nobody knows about it yet. Input validation decides what a file may
contain; confinement decides what the process can still do when that decision
was wrong.

### Why `seccompiler`

The workspace default is `unsafe_code = "deny"`. Only `libs/rust/ipc_os` and
`libs/rust/process_os` are exempt, listed in `security/rust-supply-chain.toml`
under `[unsafe] allowed_first_party_crates`, and each exemption costs a
package-local `SAFETY.md`, per-block invariants, Miri, sanitizer coverage, fuzz
coverage, and explicit OWNERS security approval (`libs/rust/UNSAFE_POLICY.md`).

Hand-rolling `libc` FFI for `seccomp(2)` and `prctl(PR_SET_NO_NEW_PRIVS)` would
have made this the third exemption and triggered that whole process for roughly
fifteen lines of syscall plumbing. `tools/qualification/rust/supply_chain.py`
scans only first-party `src/**/*.rs`, so a safe-wrapper crate keeps that
plumbing on the far side of a dependency edge and `sandbox_os` inherits the
workspace deny. That is the decisive property, and it is why the choice of
backing crate is a supply-chain question rather than an ergonomics one.

Three candidates were compared on dependency and build-tooling footprint,
because that is the cost this repository actually pays:

| | `seccompiler` 0.5.0 | `libseccomp` 0.4.0 | `foundations` 5.9.1 `security` |
|---|---|---|---|
| License | Apache-2.0 OR BSD-3-Clause | MIT OR Apache-2.0 | BSD-3-Clause |
| Non-optional deps | `libc` only | `libseccomp-sys`, `bitflags` | `bindgen`, `cc`, `once_cell` |
| Build script | none | yes (`libseccomp-sys`) | yes (`bindgen` + `cc`) |
| Host toolchain | none | system `libseccomp` C library | `libclang` at build time |
| New crates in `Cargo.lock` | 1 | several plus a C library | dozens |

`seccompiler` wins on every column. It is the seccomp-BPF compiler the
Firecracker/rust-vmm project uses, it is pure Rust, `libc` is its only
non-optional dependency and was already pinned at `=0.2.183` (it requires
`^0.2.153`), and `default-features = false` drops the optional `serde`/
`serde_json` JSON frontend, which this repository does not want: policies here
are Rust values checked by the compiler, not documents parsed at runtime.

Both `libseccomp` and `foundations`' `security` feature statically or
dynamically bind the libseccomp C library. Nix owns toolchains here
(`tools/build/nix/versions.nix`, `toolchain-manifest.json`), so either choice
widens the toolchain contract — `libclang` for the `bindgen` path, a C library in
the runtime closure for the other — to obtain a filter compiler we can have in
pure Rust. Adding a C dependency to the layer whose job is containment is also
the wrong direction on its own terms.

### `foundations`: `security` modelled on, crate declined

`foundations`' security API is good and this crate's shape is taken from it: an
`allow_list!` macro that names bare syscalls, composable named sets (its
`common_syscall_allow_lists`, our `profiles::PROCESS_BASICS`, `FILE_IO`,
`ASYNC_IO`, `NETWORK`), and one explicit `enable_syscall_sandboxing`-style entry
point rather than an import side effect. Reimplementing that shape against
`seccompiler` costs about 200 lines and avoids the dependency edge entirely.

Taking the crate would also not have been free even with
`default-features = false, features = ["security"]`. `foundations`' default
features are `platform-common-default` plus `security`, and
`platform-common-default` expands to `settings`, `jemalloc`, `telemetry`, `cli`,
`testing`, `sentry` and two panic-behaviour flags. Turning them off works at the
feature level, but the repository would still be carrying a service-framework
dependency in order to use one leaf of it, and every future `foundations` bump
would have to be re-argued against the features we do not want.

### `telemetry` declined

`foundations`' `telemetry` feature expands to `logging`, `memory-profiling`,
`metrics`, `tracing`, `telemetry-server`, `client-telemetry` and
`telemetry-otlp-grpc`, which together pull `slog` + `slog-async` + `slog-json` +
`slog-term`, `prometheus` + `prometheus-client` + `prometools`, `cf-rustracing`
+ `cf-rustracing-jaeger` + `opentelemetry-proto`, `hyper` + `hyper-util` +
`socket2` + `matchit`, `tokio`, `tonic` + `tonic-prost`, `governor`, and
`tikv-jemallocator` by way of `memory-profiling`.

Three reasons to decline, in order of weight.

First, `libs/rust/SECURITY.md` states that foundation crates create no ambient
async runtime, global thread pool, or hidden provider client. That invariant is
the reason `sandbox_os` makes installation an explicit call rather than a
`ctor`/`lazy_static` side effect, and adopting a telemetry stack that installs a
global logger, a global metric registry, an HTTP telemetry server and jemalloc
profiling hooks would contradict the same document in the same change.

Second, it would displace `libs/rust/telemetry` and re-introduce exactly the
Go↔Rust vocabulary drift that PR #99 (fault taxonomy across proto, Go and Rust)
and PR #104 (servicekit lifecycle parity) just eliminated. Those two changes
made one taxonomy and one lifecycle contract true in both languages; a
Rust-only telemetry vocabulary imported from another organization's service
framework would make it two again, at the layer where cross-language parity is
most expensive to lose.

Third, `RUNTIME_STACK.md` already pins one async stack — Tokio for execution,
Tonic/Prost for generated gRPC, Tower for bounded middleware, Bytes for network
buffers. `telemetry` brings a second, overlapping stack plus jemalloc as a hard
allocator choice, and its transitive closure is one this repository has not
audited under `deny.toml` and `security/rust-supply-chain.toml`.

### `settings` declined

`foundations`' `settings` feature is a YAML-first configuration model with its
own derive macros, `serde_path_to_error`, `serde_yaml`, `yaml-merge-keys`,
`indexmap` and `zeroize`. ADR-0023 already decided that composable profiles
resolve to one canonical configuration document and digest per run/process, and
that runs, checkpoints and releases reference that digest. That decision is
unaffected by ADR-0024 and remains in force. A second configuration authority
with a different merge model and a different serialization stack would compete
with it, and the losing side would be whichever one the next author happened to
read first.

### Why the policy is a name list, not a number list

Syscall numbers are architecture-specific — `openat` is 257 on x86_64 and 56 on
aarch64 — so the portable half of the crate carries names and the Linux backend
resolves them from a closed table. A name the table does not know is a hard
error at install time, never a silently dropped rule, because a dropped rule
changes the filter in a direction nobody chose. `arch_prctl` is the one entry
that legitimately exists on only one architecture, and it is `cfg`-gated in both
the profile set and the resolver so the two cannot disagree.

## Consequences

`libs/rust/sandbox_os` is a new layer-0 crate with one internal edge (`faults`)
and one external, Linux-only edge (`seccompiler`, plus `libc` for its syscall
constants). `libs/rust/worker_runtime` gains a dependency on it and a
`start_confined` entry point; `start()` is now defined as
`start_confined(&SandboxRequest::Disabled)`, so running unconfined is a spelled
decision rather than the absence of one.

The allowlist is bounded at 2048 entries — roughly four times the whole Linux
syscall table — because an unbounded accumulator is the shape this repository
refuses to ship, and because a policy that has grown past every syscall the
kernel has has stopped confining anything. Installation additionally fails when
the compiled program exceeds the kernel's `BPF_MAXINSNS`, and both bounds
surface as faults rather than as a weaker filter.

Profiles are coarse on purpose. A per-call-site allowlist would be tighter and
would also stop being maintained: the only allowlist anyone keeps correct is one
a reviewer can read in full and reason about as a capability. The trade is
accepted, and the capabilities that matter — `execve`, `ptrace`, `socket`,
`prctl`, `mount`, `unshare`, `setns`, `bpf`, `perf_event_open`,
`process_vm_readv`, `keyctl`, `chroot`, `pivot_root`, `init_module`,
`kexec_load`, `setuid`, `setgid` — are absent from every set and asserted absent
by `libs/rust/sandbox_os/tests/policy.rs`.

Enabling `SandboxRequest::Required` for a specific deployment is a separate,
per-process decision. This ADR ships the mechanism, the profiles, and the
worker-boundary entry point; a composition root that turns it on owns the
qualification that its workload runs inside the profile it selected.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does
not change the accepted architecture. In particular, adding a first-party
`unsafe` seccomp adapter, adding a non-terminating `ViolationAction`, or making
installation best-effort would each reverse a decision recorded here.
