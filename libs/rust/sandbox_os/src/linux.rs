// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Linux seccomp-BPF backend.
//!
//! Compiles a [`SandboxPolicy`] to a BPF program and installs it. The syscall
//! *numbers*, the `prctl(PR_SET_NO_NEW_PRIVS)` that must precede installation,
//! and the `seccomp(2)` call itself all live behind `seccompiler`'s safe API,
//! which is the reason this crate is not a third entry in
//! `libs/rust/UNSAFE_POLICY.md`. ADR-0027 records that evaluation.
//!
//! Resolution is a closed table. A name it does not know is an error, so a
//! typo in a profile is a startup failure on Linux rather than a rule that
//! quietly disappears from the filter.
//!
//! The `libc::SYS_*` constants are `c_long`, which is `i64` on every 64-bit
//! Linux target this repository declares (`x86_64-linux`, `aarch64-linux`).

use crate::policy::{SandboxPolicy, Scope, ViolationAction};
use crate::report::EnforcementReport;
use mindclade_faults::{Code, Fault, FaultResult};
use seccompiler::{BpfProgram, SeccompAction, SeccompFilter, SeccompRule, TargetArch};
use std::collections::BTreeMap;

/// Compiles and installs `policy`, or fails.
pub(crate) fn install(policy: &SandboxPolicy) -> FaultResult<EnforcementReport> {
    let program = compile(policy)?;
    let instructions = program.len();
    let applied = match policy.scope() {
        Scope::Process => seccompiler::apply_filter_all_threads(&program),
        Scope::CallingThread => seccompiler::apply_filter(&program),
    };
    applied.map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "kernel refused the seccomp-BPF filter; the process is not confined",
        )
        .with_context(
            "program_instructions",
            u64::try_from(instructions).unwrap_or(u64::MAX),
        )
        .with_source(error)
    })?;
    Ok(EnforcementReport::new(
        policy.allowed().len(),
        instructions,
        policy.scope(),
        policy.violation(),
    ))
}

fn compile(policy: &SandboxPolicy) -> FaultResult<BpfProgram> {
    let architecture = target_architecture()?;
    let mut rules: BTreeMap<i64, Vec<SeccompRule>> = BTreeMap::new();
    for name in policy.allowed().names() {
        // An empty rule chain matches the syscall unconditionally, so the
        // filter is a pure allowlist by number with no argument inspection.
        rules.insert(resolve(name)?, Vec::new());
    }
    let filter = SeccompFilter::new(
        rules,
        mismatch_action(policy.violation()),
        SeccompAction::Allow,
        architecture,
    )
    .map_err(|error| {
        Fault::new(Code::InvalidArgument, "seccomp policy is not compilable").with_source(error)
    })?;
    BpfProgram::try_from(filter).map_err(|error| {
        Fault::new(
            Code::ResourceExhausted,
            "seccomp policy does not fit the kernel's BPF program limit",
        )
        .with_source(error)
    })
}

fn target_architecture() -> FaultResult<TargetArch> {
    TargetArch::try_from(std::env::consts::ARCH).map_err(|error| {
        Fault::new(
            Code::Unimplemented,
            "seccomp-BPF is not supported on this architecture",
        )
        .with_context("architecture", std::env::consts::ARCH)
        .with_source(error)
    })
}

fn mismatch_action(violation: ViolationAction) -> SeccompAction {
    match violation {
        ViolationAction::KillProcess => SeccompAction::KillProcess,
        ViolationAction::Trap => SeccompAction::Trap,
    }
}

fn resolve(name: &str) -> FaultResult<i64> {
    process_basics(name)
        .or_else(|| architecture_basics(name))
        .or_else(|| file_io(name))
        .or_else(|| async_io(name))
        .or_else(|| network(name))
        .ok_or_else(|| {
            Fault::new(
                Code::InvalidArgument,
                "system call is not in the platform table",
            )
            .with_context("syscall", name.to_owned())
            .with_context("architecture", std::env::consts::ARCH)
        })
}

fn process_basics(name: &str) -> Option<i64> {
    Some(match name {
        "exit" => libc::SYS_exit,
        "exit_group" => libc::SYS_exit_group,
        "rt_sigreturn" => libc::SYS_rt_sigreturn,
        "rt_sigaction" => libc::SYS_rt_sigaction,
        "rt_sigprocmask" => libc::SYS_rt_sigprocmask,
        "rt_sigtimedwait" => libc::SYS_rt_sigtimedwait,
        "rt_sigpending" => libc::SYS_rt_sigpending,
        "rt_sigsuspend" => libc::SYS_rt_sigsuspend,
        "sigaltstack" => libc::SYS_sigaltstack,
        "tgkill" => libc::SYS_tgkill,
        "futex" => libc::SYS_futex,
        "futex_waitv" => libc::SYS_futex_waitv,
        "set_robust_list" => libc::SYS_set_robust_list,
        "get_robust_list" => libc::SYS_get_robust_list,
        "set_tid_address" => libc::SYS_set_tid_address,
        "rseq" => libc::SYS_rseq,
        "membarrier" => libc::SYS_membarrier,
        "restart_syscall" => libc::SYS_restart_syscall,
        "clone" => libc::SYS_clone,
        "clone3" => libc::SYS_clone3,
        "sched_yield" => libc::SYS_sched_yield,
        "sched_getaffinity" => libc::SYS_sched_getaffinity,
        "sched_setaffinity" => libc::SYS_sched_setaffinity,
        "sched_getparam" => libc::SYS_sched_getparam,
        "sched_get_priority_max" => libc::SYS_sched_get_priority_max,
        "sched_get_priority_min" => libc::SYS_sched_get_priority_min,
        "mmap" => libc::SYS_mmap,
        "munmap" => libc::SYS_munmap,
        "mprotect" => libc::SYS_mprotect,
        "mremap" => libc::SYS_mremap,
        "madvise" => libc::SYS_madvise,
        "brk" => libc::SYS_brk,
        "mlock" => libc::SYS_mlock,
        "munlock" => libc::SYS_munlock,
        "gettid" => libc::SYS_gettid,
        "getpid" => libc::SYS_getpid,
        "getppid" => libc::SYS_getppid,
        "getuid" => libc::SYS_getuid,
        "geteuid" => libc::SYS_geteuid,
        "getgid" => libc::SYS_getgid,
        "getegid" => libc::SYS_getegid,
        "clock_gettime" => libc::SYS_clock_gettime,
        "clock_getres" => libc::SYS_clock_getres,
        "clock_nanosleep" => libc::SYS_clock_nanosleep,
        "nanosleep" => libc::SYS_nanosleep,
        "gettimeofday" => libc::SYS_gettimeofday,
        "getrandom" => libc::SYS_getrandom,
        "prlimit64" => libc::SYS_prlimit64,
        "getrlimit" => libc::SYS_getrlimit,
        "uname" => libc::SYS_uname,
        "sysinfo" => libc::SYS_sysinfo,
        _ => return None,
    })
}

#[cfg(target_arch = "x86_64")]
fn architecture_basics(name: &str) -> Option<i64> {
    Some(match name {
        "arch_prctl" => libc::SYS_arch_prctl,
        _ => return None,
    })
}

#[cfg(not(target_arch = "x86_64"))]
fn architecture_basics(_name: &str) -> Option<i64> {
    // `profiles::ARCHITECTURE_BASICS` is empty on these targets, so no name
    // reaches here. The function exists so that `resolve` has one shape.
    None
}

fn file_io(name: &str) -> Option<i64> {
    Some(match name {
        "read" => libc::SYS_read,
        "write" => libc::SYS_write,
        "pread64" => libc::SYS_pread64,
        "pwrite64" => libc::SYS_pwrite64,
        "readv" => libc::SYS_readv,
        "writev" => libc::SYS_writev,
        "preadv" => libc::SYS_preadv,
        "pwritev" => libc::SYS_pwritev,
        "preadv2" => libc::SYS_preadv2,
        "pwritev2" => libc::SYS_pwritev2,
        "openat" => libc::SYS_openat,
        "openat2" => libc::SYS_openat2,
        "close" => libc::SYS_close,
        "close_range" => libc::SYS_close_range,
        "lseek" => libc::SYS_lseek,
        "fstat" => libc::SYS_fstat,
        "newfstatat" => libc::SYS_newfstatat,
        "statx" => libc::SYS_statx,
        "statfs" => libc::SYS_statfs,
        "fstatfs" => libc::SYS_fstatfs,
        "fsync" => libc::SYS_fsync,
        "fdatasync" => libc::SYS_fdatasync,
        "ftruncate" => libc::SYS_ftruncate,
        "fallocate" => libc::SYS_fallocate,
        "sync_file_range" => libc::SYS_sync_file_range,
        "getdents64" => libc::SYS_getdents64,
        "getcwd" => libc::SYS_getcwd,
        "readlinkat" => libc::SYS_readlinkat,
        "faccessat" => libc::SYS_faccessat,
        "faccessat2" => libc::SYS_faccessat2,
        "unlinkat" => libc::SYS_unlinkat,
        "renameat" => libc::SYS_renameat,
        "renameat2" => libc::SYS_renameat2,
        "mkdirat" => libc::SYS_mkdirat,
        "linkat" => libc::SYS_linkat,
        "fchmod" => libc::SYS_fchmod,
        "fchmodat" => libc::SYS_fchmodat,
        "fcntl" => libc::SYS_fcntl,
        "flock" => libc::SYS_flock,
        "dup" => libc::SYS_dup,
        "dup3" => libc::SYS_dup3,
        "pipe2" => libc::SYS_pipe2,
        "ioctl" => libc::SYS_ioctl,
        "copy_file_range" => libc::SYS_copy_file_range,
        "memfd_create" => libc::SYS_memfd_create,
        "utimensat" => libc::SYS_utimensat,
        _ => return None,
    })
}

fn async_io(name: &str) -> Option<i64> {
    Some(match name {
        "epoll_create1" => libc::SYS_epoll_create1,
        "epoll_ctl" => libc::SYS_epoll_ctl,
        "epoll_pwait" => libc::SYS_epoll_pwait,
        "epoll_pwait2" => libc::SYS_epoll_pwait2,
        "eventfd2" => libc::SYS_eventfd2,
        "timerfd_create" => libc::SYS_timerfd_create,
        "timerfd_settime" => libc::SYS_timerfd_settime,
        "timerfd_gettime" => libc::SYS_timerfd_gettime,
        "ppoll" => libc::SYS_ppoll,
        "pselect6" => libc::SYS_pselect6,
        "signalfd4" => libc::SYS_signalfd4,
        _ => return None,
    })
}

fn network(name: &str) -> Option<i64> {
    Some(match name {
        "socket" => libc::SYS_socket,
        "socketpair" => libc::SYS_socketpair,
        "connect" => libc::SYS_connect,
        "bind" => libc::SYS_bind,
        "listen" => libc::SYS_listen,
        "accept4" => libc::SYS_accept4,
        "sendto" => libc::SYS_sendto,
        "recvfrom" => libc::SYS_recvfrom,
        "sendmsg" => libc::SYS_sendmsg,
        "recvmsg" => libc::SYS_recvmsg,
        "sendmmsg" => libc::SYS_sendmmsg,
        "recvmmsg" => libc::SYS_recvmmsg,
        "getsockname" => libc::SYS_getsockname,
        "getpeername" => libc::SYS_getpeername,
        "getsockopt" => libc::SYS_getsockopt,
        "setsockopt" => libc::SYS_setsockopt,
        "shutdown" => libc::SYS_shutdown,
        _ => return None,
    })
}
