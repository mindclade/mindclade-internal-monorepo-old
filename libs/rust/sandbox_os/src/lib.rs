// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Kernel-enforced syscall confinement for processes that handle untrusted
//! input.
//!
//! Everything else in this repository that looks like isolation is cooperative:
//! process groups and bounded termination (`mindclade_process_os`), sealed
//! memfd segments (`mindclade_ipc_os`), and userspace budget accounting
//! (`mindclade_runtime_core::budget`). All of it depends on the confined code
//! behaving. This crate does not: it hands the kernel an allowlist of system
//! calls and the kernel refuses everything else, whether or not the process
//! agrees.
//!
//! It sits *beneath* input validation rather than beside it. `bounded_parse`
//! and `bio_formats` decide what a FASTA or A3M file is allowed to contain;
//! this decides what the process can still do on the day one of those bounds is
//! wrong.
//!
//! # Fail closed
//!
//! [`install`] returns [`Confinement::NotRequested`] when confinement was not
//! asked for, [`Confinement::Enforced`] when a filter is installed, and an
//! error in every other case. There is no "requested, unavailable, continued
//! anyway" path, and no log-and-proceed fallback: a sandbox that silently does
//! not install is worse than no sandbox, because the operator believes there is
//! one. On a non-Linux host a required policy is [`Code::Unimplemented`], not a
//! warning.
//!
//! # No ambient state
//!
//! Installing a filter is irreversibly process-global, so it happens only when
//! a composition root calls [`install`]. Nothing here runs on import, and this
//! crate creates no runtime, thread pool or client — see `libs/rust/SECURITY.md`.
//!
//! # Portability
//!
//! seccomp-BPF is Linux-only. The policy types compile everywhere so that a
//! composition root reads identically on every host; only the backend is
//! `#[cfg(target_os = "linux")]`, following `mindclade_ipc_os` and
//! `mindclade_process_os`.
//!
//! ```no_run
//! use mindclade_sandbox_os::{SandboxRequest, profiles};
//!
//! # fn main() -> mindclade_faults::FaultResult<()> {
//! let request = SandboxRequest::Required(profiles::untrusted_input_worker()?);
//! // Refuses to return Ok if the filter could not be installed.
//! let confinement = mindclade_sandbox_os::install(&request)?;
//! assert!(confinement.is_enforced());
//! # Ok(())
//! # }
//! ```
#![forbid(unsafe_code)]

mod allowlist;
#[cfg(target_os = "linux")]
mod linux;
mod policy;
pub mod profiles;
mod report;

pub use allowlist::{Syscall, SyscallAllowList};
pub use policy::{MANDATORY_SYSCALLS, SandboxPolicy, SandboxPolicyBuilder, Scope, ViolationAction};
pub use report::{Confinement, EnforcementReport};

use mindclade_faults::FaultResult;

/// What a composition root asked for.
///
/// The two variants are the whole reason this is an enum rather than an
/// `Option<SandboxPolicy>`: `Disabled` is a decision somebody made and can be
/// read back out of a resolved configuration, while `Required` is a promise
/// that the process will not start unconfined.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SandboxRequest {
    /// Run unconfined. No filter is installed and `install` reports that.
    Disabled,
    /// Install this policy or refuse to start.
    Required(SandboxPolicy),
}

/// The single explicit entry point that installs kernel-enforced confinement.
///
/// Call this once, from a composition root, before any untrusted input is
/// touched. It is irreversible for the life of the process, which is the
/// property that makes it worth anything.
///
/// # Errors
///
/// Returns a fault when a policy was required and could not be installed: the
/// host has no seccomp-BPF, a syscall name is not in the platform table, the
/// compiled program exceeds what the kernel accepts, or the kernel rejects the
/// filter. Never returns `Ok` for a required policy that is not in force.
pub fn install(request: &SandboxRequest) -> FaultResult<Confinement> {
    match request {
        SandboxRequest::Disabled => Ok(Confinement::NotRequested),
        SandboxRequest::Required(policy) => install_policy(policy).map(Confinement::Enforced),
    }
}

#[cfg(target_os = "linux")]
fn install_policy(policy: &SandboxPolicy) -> FaultResult<EnforcementReport> {
    linux::install(policy)
}

#[cfg(not(target_os = "linux"))]
fn install_policy(policy: &SandboxPolicy) -> FaultResult<EnforcementReport> {
    use mindclade_faults::{Code, Fault};

    // Deliberately an error rather than a no-op. A macOS developer build that
    // silently ran unconfined would make the Linux deployment's confinement
    // untested locally and invisible in review; a caller that genuinely wants
    // to run unconfined here says so with `SandboxRequest::Disabled`.
    Err(Fault::new(
        Code::Unimplemented,
        "kernel-enforced syscall confinement requires Linux seccomp-BPF",
    )
    .with_context("target_os", std::env::consts::OS)
    .with_context(
        "requested_syscalls",
        u64::try_from(policy.allowed().len()).unwrap_or(u64::MAX),
    ))
}
