// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Policy construction and the fail-closed contract, on every host.

use mindclade_faults::Code;
use mindclade_sandbox_os::{
    Confinement, MANDATORY_SYSCALLS, SandboxPolicy, SandboxRequest, Scope, Syscall,
    SyscallAllowList, ViolationAction, profiles,
};

#[test]
fn a_policy_that_admits_nothing_is_rejected() {
    let fault = SandboxPolicy::builder()
        .build()
        .expect_err("an empty allowlist must not compile into a policy");
    assert_eq!(fault.code(), Code::InvalidArgument);
}

#[test]
fn a_policy_that_cannot_terminate_is_rejected() {
    // Everything a worker needs except the calls that let it exit. A filter
    // like this turns every clean shutdown into a kill, so it is a defect at
    // build time rather than a surprise during drain.
    let fault = SandboxPolicy::builder()
        .allow(profiles::FILE_IO)
        .expect("file IO is within the bound")
        .build()
        .expect_err("a policy without exit syscalls must be rejected");
    assert_eq!(fault.code(), Code::InvalidArgument);
    assert!(
        fault.message().contains("orderly termination"),
        "fault must name the reason: {fault}"
    );
}

#[test]
fn the_allowlist_is_bounded() {
    // Not decoration. The bound is what stops a policy accumulating without
    // limit until it admits more than the kernel has syscalls, at which point
    // it has stopped confining anything.
    let mut list = SyscallAllowList::new();
    let filler: Vec<Syscall> = (0..SyscallAllowList::MAXIMUM_ENTRIES)
        .map(|index| Syscall::new(Box::leak(format!("synthetic_{index}").into_boxed_str())))
        .collect();
    list = list.allowing(&filler).expect("fill exactly to the bound");
    assert_eq!(list.len(), SyscallAllowList::MAXIMUM_ENTRIES);

    let fault = list
        .allowing(&[Syscall::new("one_too_many")])
        .expect_err("growth past the bound must be rejected");
    assert_eq!(fault.code(), Code::ResourceExhausted);
}

#[test]
fn readmitting_an_admitted_syscall_at_the_bound_is_not_growth() {
    let filler: Vec<Syscall> = (0..SyscallAllowList::MAXIMUM_ENTRIES)
        .map(|index| Syscall::new(Box::leak(format!("bounded_{index}").into_boxed_str())))
        .collect();
    let list = SyscallAllowList::new()
        .allowing(&filler)
        .expect("fill exactly to the bound")
        .allowing(&filler)
        .expect("re-admitting the same set must not count as growth");
    assert_eq!(list.len(), SyscallAllowList::MAXIMUM_ENTRIES);
}

#[test]
fn the_worker_profile_denies_the_capabilities_it_is_meant_to_deny() {
    let policy = profiles::untrusted_input_worker().expect("build the worker profile");
    for denied in [
        "execve",
        "execveat",
        "ptrace",
        "socket",
        "connect",
        "prctl",
        "seccomp",
        "mount",
        "unshare",
        "setns",
        "bpf",
        "perf_event_open",
        "process_vm_readv",
        "keyctl",
        "chroot",
        "pivot_root",
        "init_module",
        "kexec_load",
        "setuid",
        "setgid",
    ] {
        assert!(
            !policy.allowed().contains(Syscall::new(denied)),
            "{denied} must not be admitted by the untrusted-input worker profile"
        );
    }
    for admitted in ["read", "write", "openat", "mmap", "futex", "exit_group"] {
        assert!(
            policy.allowed().contains(Syscall::new(admitted)),
            "{admitted} must be admitted or the worker cannot run at all"
        );
    }
}

#[test]
fn the_networked_profile_is_the_worker_profile_plus_sockets() {
    let worker = profiles::untrusted_input_worker().expect("worker profile");
    let networked = profiles::networked_worker().expect("networked profile");
    assert!(!worker.allowed().contains(Syscall::new("socket")));
    assert!(networked.allowed().contains(Syscall::new("socket")));
    for name in worker.allowed().names() {
        assert!(
            networked.allowed().contains(Syscall::new(name)),
            "the networked profile must be a superset; {name} is missing"
        );
    }
}

#[test]
fn every_profile_carries_the_mandatory_syscalls() {
    for policy in [
        profiles::untrusted_input_worker().expect("worker profile"),
        profiles::networked_worker().expect("networked profile"),
    ] {
        for mandatory in MANDATORY_SYSCALLS {
            assert!(policy.allowed().contains(*mandatory));
        }
    }
}

#[test]
fn defaults_are_the_strict_ones() {
    let policy = profiles::untrusted_input_worker().expect("worker profile");
    assert_eq!(policy.scope(), Scope::Process);
    assert_eq!(policy.violation(), ViolationAction::KillProcess);
}

#[test]
fn not_requesting_confinement_is_distinct_from_requesting_it() {
    // The distinction the whole API exists to carry. `Disabled` succeeds and
    // reports that nothing is installed; it never masquerades as enforcement.
    let outcome = mindclade_sandbox_os::install(&SandboxRequest::Disabled)
        .expect("an explicit opt-out is not a failure");
    assert_eq!(outcome, Confinement::NotRequested);
    assert!(!outcome.is_enforced());
    assert!(outcome.report().is_none());
}

/// A required policy either takes effect or fails; it is never silently skipped.
///
/// On Linux this is the enforcement path and is covered by `enforcement.rs`.
/// Everywhere else there is no seccomp-BPF at all, so the only fail-closed
/// answer is a typed `Unimplemented` fault — which is what this asserts, and
/// what makes an aarch64-darwin developer build refuse rather than quietly run
/// the untrusted-input path unconfined.
#[cfg(not(target_os = "linux"))]
#[test]
fn a_required_policy_on_a_host_without_seccomp_refuses() {
    let request =
        SandboxRequest::Required(profiles::untrusted_input_worker().expect("worker profile"));
    let fault = mindclade_sandbox_os::install(&request)
        .expect_err("confinement is unavailable here and must not report success");
    assert_eq!(fault.code(), Code::Unimplemented);
    assert!(
        fault.message().contains("Linux"),
        "fault must say why: {fault}"
    );
}
