// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Reusable allowlist sets and the workload policies built from them.
//!
//! Sets are additive and deliberately coarse. A per-call-site allowlist would
//! be tighter and would also be wrong within a week: the only allowlist anyone
//! maintains is one that a reviewer can read in full and reason about as a
//! capability ("this process may touch files; it may not open sockets or
//! execute programs").
//!
//! Everything omitted here is denied, which is the interesting half. `execve`,
//! `execveat`, `ptrace`, `mount`, `unshare`, `setns`, `bpf`, `perf_event_open`,
//! `process_vm_readv`, `keyctl`, `chroot`, `pivot_root`, `init_module`,
//! `kexec_load`, `seccomp`, `prctl`, `setuid` and `setgid` appear in no set. A
//! parser defect that reaches arbitrary-code-execution inside one of these
//! processes cannot spawn a shell, attach a debugger to a sibling, rewrite the
//! filter, or acquire a capability it was not started with.

use crate::allow_list;
use crate::allowlist::Syscall;
use crate::policy::{MANDATORY_SYSCALLS, SandboxPolicy};
use mindclade_faults::FaultResult;

/// Threads, memory, signals, scheduling, time and exit.
///
/// The floor beneath any Rust process: without these it cannot start a Tokio
/// worker, take a lock, allocate, or die.
pub static PROCESS_BASICS: &[Syscall] = &allow_list![
    exit,
    exit_group,
    rt_sigreturn,
    rt_sigaction,
    rt_sigprocmask,
    rt_sigtimedwait,
    rt_sigpending,
    rt_sigsuspend,
    sigaltstack,
    tgkill,
    futex,
    futex_waitv,
    set_robust_list,
    get_robust_list,
    set_tid_address,
    rseq,
    membarrier,
    restart_syscall,
    clone,
    clone3,
    sched_yield,
    sched_getaffinity,
    sched_setaffinity,
    sched_getparam,
    sched_get_priority_max,
    sched_get_priority_min,
    mmap,
    munmap,
    mprotect,
    mremap,
    madvise,
    brk,
    mlock,
    munlock,
    gettid,
    getpid,
    getppid,
    getuid,
    geteuid,
    getgid,
    getegid,
    clock_gettime,
    clock_getres,
    clock_nanosleep,
    nanosleep,
    gettimeofday,
    getrandom,
    prlimit64,
    getrlimit,
    uname,
    sysinfo,
];

/// Architecture-specific calls the C runtime needs and that do not exist
/// everywhere.
///
/// `arch_prctl` is how glibc installs the thread pointer on x86_64; a filter
/// that omits it kills the process at the first `std::thread::spawn`. aarch64
/// sets the TLS register in `clone` and has no such syscall, so the set is
/// empty there and the resolver never sees the name.
#[cfg(target_arch = "x86_64")]
pub static ARCHITECTURE_BASICS: &[Syscall] = &allow_list![arch_prctl];

/// Architecture-specific calls the C runtime needs; empty on this target.
#[cfg(not(target_arch = "x86_64"))]
pub static ARCHITECTURE_BASICS: &[Syscall] = &[];

/// Regular-file and directory access, including the atomic-publish path
/// (`renameat`/`fsync`) that `atomic_fs` and `checkpoint_io` depend on.
///
/// Note what is *not* here: `symlinkat`, `fchown`, `fchownat`, `chdir` and
/// `chroot`. A confined worker resolves paths it was given; it does not
/// re-point the namespace it resolves them in.
pub static FILE_IO: &[Syscall] = &allow_list![
    read,
    write,
    pread64,
    pwrite64,
    readv,
    writev,
    preadv,
    pwritev,
    preadv2,
    pwritev2,
    openat,
    openat2,
    close,
    close_range,
    lseek,
    fstat,
    newfstatat,
    statx,
    statfs,
    fstatfs,
    fsync,
    fdatasync,
    ftruncate,
    fallocate,
    sync_file_range,
    getdents64,
    getcwd,
    readlinkat,
    faccessat,
    faccessat2,
    unlinkat,
    renameat,
    renameat2,
    mkdirat,
    linkat,
    fchmod,
    fchmodat,
    fcntl,
    flock,
    dup,
    dup3,
    pipe2,
    ioctl,
    copy_file_range,
    memfd_create,
    utimensat,
];

/// Readiness notification, timers and event descriptors.
///
/// This is what a Tokio reactor drives itself with. It admits no new
/// descriptors of its own — `eventfd2` and `timerfd_create` produce
/// process-local objects, not channels out.
pub static ASYNC_IO: &[Syscall] = &allow_list![
    epoll_create1,
    epoll_ctl,
    epoll_pwait,
    epoll_pwait2,
    eventfd2,
    timerfd_create,
    timerfd_settime,
    timerfd_gettime,
    ppoll,
    pselect6,
    signalfd4,
];

/// Sockets.
///
/// Separated from everything else precisely so that the default worker profile
/// can leave it out. A process that parses an untrusted FASTA file has no
/// business creating a socket, and denying the capability is stronger than
/// auditing the code that might use it.
pub static NETWORK: &[Syscall] = &allow_list![
    socket,
    socketpair,
    connect,
    bind,
    listen,
    accept4,
    sendto,
    recvfrom,
    sendmsg,
    recvmsg,
    sendmmsg,
    recvmmsg,
    getsockname,
    getpeername,
    getsockopt,
    setsockopt,
    shutdown,
];

/// Policy for a worker that reads untrusted model inputs and scientific files
/// over already-open descriptors and the local filesystem.
///
/// This is the containment layer beneath `bounded_parse`/`bio_formats`: it is
/// what the process still cannot do on the day a parser bound is wrong.
pub fn untrusted_input_worker() -> FaultResult<SandboxPolicy> {
    SandboxPolicy::builder()
        .allow(MANDATORY_SYSCALLS)?
        .allow(PROCESS_BASICS)?
        .allow(ARCHITECTURE_BASICS)?
        .allow(FILE_IO)?
        .allow(ASYNC_IO)?
        .build()
}

/// [`untrusted_input_worker`] plus sockets, for a worker that also holds its
/// own control-plane or gRPC transport rather than inheriting descriptors.
pub fn networked_worker() -> FaultResult<SandboxPolicy> {
    SandboxPolicy::builder()
        .allow(MANDATORY_SYSCALLS)?
        .allow(PROCESS_BASICS)?
        .allow(ARCHITECTURE_BASICS)?
        .allow(FILE_IO)?
        .allow(ASYNC_IO)?
        .allow(NETWORK)?
        .build()
}
