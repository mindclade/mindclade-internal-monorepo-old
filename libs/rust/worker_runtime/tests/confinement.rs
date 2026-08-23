// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The worker refuses to reach `Ready` unconfined once confinement is required.

use mindclade_faults::Code;
use mindclade_runtime_core::{Budget, ResourceKind, ResourceVector};
use mindclade_sandbox_os::{Confinement, SandboxRequest};
use mindclade_worker_protocol::WorkerState;
use mindclade_worker_runtime::WorkerRuntime;
use mindclade_worker_runtime::confinement;

fn worker() -> WorkerRuntime {
    WorkerRuntime::new(Budget::root(
        "node",
        ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 1024),
    ))
}

#[test]
fn starting_without_a_request_reports_that_nothing_is_installed() {
    let runtime = worker();
    let confinement = runtime
        .start_confined(&SandboxRequest::Disabled)
        .expect("an explicit opt-out is not a failure");
    assert_eq!(confinement, Confinement::NotRequested);
    assert_eq!(runtime.state(), WorkerState::Ready);
}

#[test]
fn plain_start_is_the_unconfined_request() {
    let runtime = worker();
    runtime.start().expect("start unconfined");
    assert_eq!(runtime.state(), WorkerState::Ready);
}

#[test]
fn the_worker_policies_build() {
    // Cheap, and it is the check that catches a profile edit that made the
    // policy invalid: without it the mistake would only surface on a Linux host
    // at process start.
    confinement::untrusted_input_policy().expect("untrusted-input worker policy");
    confinement::networked_policy().expect("networked worker policy");
    assert!(matches!(
        confinement::untrusted_input_request().expect("request"),
        SandboxRequest::Required(_)
    ));
    assert!(matches!(
        confinement::networked_request().expect("request"),
        SandboxRequest::Required(_)
    ));
}

/// The fail-closed contract at the process boundary, observable on any host
/// without seccomp-BPF.
///
/// This is the assertion that matters most in this file: the worker asked for
/// kernel confinement, could not have it, and did **not** become `Ready`. A
/// supervisor polling state sees `Failed`, so untrusted input is never routed
/// to a worker that believes it is confined and is not.
#[cfg(not(target_os = "linux"))]
#[test]
fn a_worker_that_cannot_be_confined_refuses_to_become_ready() {
    let runtime = worker();
    let request = confinement::untrusted_input_request().expect("request");
    let fault = runtime
        .start_confined(&request)
        .expect_err("confinement is unavailable on this host");
    assert_eq!(fault.code(), Code::Unimplemented);
    assert_eq!(runtime.state(), WorkerState::Failed);
    assert_ne!(runtime.state(), WorkerState::Ready);
}

/// The same contract on Linux, where the reason for the refusal is a policy the
/// kernel cannot be given rather than a kernel that has no seccomp at all.
#[cfg(target_os = "linux")]
#[test]
fn a_worker_that_cannot_be_confined_refuses_to_become_ready() {
    use mindclade_sandbox_os::{MANDATORY_SYSCALLS, SandboxPolicy, Syscall};

    let policy = SandboxPolicy::builder()
        .allow(MANDATORY_SYSCALLS)
        .expect("mandatory syscalls")
        .allow(&[Syscall::new("no_such_system_call")])
        .expect("within the allowlist bound")
        .build()
        .expect("the builder cannot know platform numbers");
    let runtime = worker();
    let fault = runtime
        .start_confined(&SandboxRequest::Required(policy))
        .expect_err("an uninstallable policy must not start the worker");
    assert_eq!(fault.code(), Code::InvalidArgument);
    assert_eq!(runtime.state(), WorkerState::Failed);
    assert_ne!(runtime.state(), WorkerState::Ready);
}
