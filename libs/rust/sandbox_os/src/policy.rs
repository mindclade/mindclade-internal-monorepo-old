// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Sandbox policy and its builder.

use crate::allow_list;
use crate::allowlist::{Syscall, SyscallAllowList};
use mindclade_faults::{Code, Fault, FaultResult};

/// System calls no admissible policy may omit.
///
/// A filter that denies these cannot let its own process die, and a process
/// that cannot return from a signal handler or call `exit_group` turns every
/// orderly shutdown into a kill. Requiring them is not a relaxation of the
/// allowlist; it rejects a policy that would have been broken in a way that
/// only shows up during termination.
pub static MANDATORY_SYSCALLS: &[Syscall] = &allow_list![exit, exit_group, rt_sigreturn];

/// Which threads the installed filter governs.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Scope {
    /// Every thread in the process, via the kernel's `TSYNC` flag.
    ///
    /// The correct choice at a composition root: a filter installed on one
    /// thread of an already-running Tokio process would leave every worker
    /// thread unconfined, which is the failure mode where a sandbox exists on
    /// paper and the untrusted parse runs outside it.
    Process,
    /// Only the calling thread.
    ///
    /// Filters are inherited across `clone`, so installing before any thread is
    /// spawned confines the whole process either way. Tests use this to keep
    /// the blast radius inside one test thread.
    CallingThread,
}

/// What the kernel does when a denied system call is attempted.
///
/// Both variants terminate. There is deliberately no variant that returns an
/// errno and lets the process continue: a filter whose violation path is
/// survivable is an advisory, and the whole point of this crate is that the
/// containment layer beneath input validation is not advisory.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ViolationAction {
    /// Kill the whole process (`SECCOMP_RET_KILL_PROCESS`).
    KillProcess,
    /// Raise `SIGSYS` on the offending thread (`SECCOMP_RET_TRAP`).
    ///
    /// Kills by default, but leaves a catchable signal for a supervisor that
    /// wants to record which syscall was attempted before dying.
    Trap,
}

/// A compiled-to-be sandbox policy: what is admitted, over what, and what
/// happens to everything else.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SandboxPolicy {
    allowed: SyscallAllowList,
    scope: Scope,
    violation: ViolationAction,
}

impl SandboxPolicy {
    /// Starts a policy builder.
    #[must_use]
    pub fn builder() -> SandboxPolicyBuilder {
        SandboxPolicyBuilder::new()
    }
    /// The admitted system calls.
    #[must_use]
    pub const fn allowed(&self) -> &SyscallAllowList {
        &self.allowed
    }
    /// Which threads the filter governs.
    #[must_use]
    pub const fn scope(&self) -> Scope {
        self.scope
    }
    /// Returns the same policy governing a different set of threads.
    ///
    /// The admitted set is unchanged — this moves the boundary, not the
    /// contents. It exists for callers that take a profile as given and know
    /// something the profile cannot: chiefly a test that must keep an installed
    /// filter inside one thread, since a filter is never removable and would
    /// otherwise outlive the case that installed it.
    #[must_use]
    pub fn rescoped(mut self, scope: Scope) -> Self {
        self.scope = scope;
        self
    }
    /// What the kernel does on a denied call.
    #[must_use]
    pub const fn violation(&self) -> ViolationAction {
        self.violation
    }
}

/// Builder for [`SandboxPolicy`].
#[derive(Clone, Debug)]
pub struct SandboxPolicyBuilder {
    allowed: SyscallAllowList,
    scope: Scope,
    violation: ViolationAction,
}

impl Default for SandboxPolicyBuilder {
    fn default() -> Self {
        Self::new()
    }
}

impl SandboxPolicyBuilder {
    /// Creates a builder that admits nothing yet.
    #[must_use]
    pub fn new() -> Self {
        Self {
            allowed: SyscallAllowList::new(),
            scope: Scope::Process,
            violation: ViolationAction::KillProcess,
        }
    }
    /// Admits a set of system calls.
    pub fn allow(mut self, syscalls: &[Syscall]) -> FaultResult<Self> {
        self.allowed = self.allowed.allowing(syscalls)?;
        Ok(self)
    }
    /// Selects which threads the filter governs.
    #[must_use]
    pub fn scope(mut self, scope: Scope) -> Self {
        self.scope = scope;
        self
    }
    /// Selects what the kernel does on a denied call.
    #[must_use]
    pub fn on_violation(mut self, violation: ViolationAction) -> Self {
        self.violation = violation;
        self
    }
    /// Validates and freezes the policy.
    pub fn build(self) -> FaultResult<SandboxPolicy> {
        if self.allowed.is_empty() {
            return Err(Fault::new(
                Code::InvalidArgument,
                "sandbox policy admits no system calls",
            ));
        }
        let missing: Vec<&'static str> = MANDATORY_SYSCALLS
            .iter()
            .filter(|syscall| !self.allowed.contains(**syscall))
            .map(|syscall| syscall.name())
            .collect();
        if !missing.is_empty() {
            return Err(Fault::new(
                Code::InvalidArgument,
                "sandbox policy omits system calls required for orderly termination",
            )
            .with_context("missing_syscalls", missing.join(",")));
        }
        Ok(SandboxPolicy {
            allowed: self.allowed,
            scope: self.scope,
            violation: self.violation,
        })
    }
}
