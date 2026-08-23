// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! What actually happened when confinement was installed.
//!
//! The distinction this type exists to carry is "no filter because nobody asked
//! for one" versus "a filter, installed and enforced". The third case —
//! somebody asked and it could not be done — is not a variant here at all; it
//! is an `Err`, so a caller cannot fall through it by accident.

use crate::policy::{Scope, ViolationAction};

/// Outcome of a confinement request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Confinement {
    /// Confinement was not requested; the process runs unconfined and knows it.
    NotRequested,
    /// A seccomp-BPF filter is installed and the kernel is enforcing it.
    Enforced(EnforcementReport),
}

impl Confinement {
    /// Whether the kernel is enforcing a filter for this process.
    #[must_use]
    pub const fn is_enforced(self) -> bool {
        matches!(self, Self::Enforced(_))
    }
    /// The enforcement detail, when a filter was installed.
    #[must_use]
    pub const fn report(self) -> Option<EnforcementReport> {
        match self {
            Self::Enforced(report) => Some(report),
            Self::NotRequested => None,
        }
    }
}

/// Details of an installed filter, for startup telemetry and for tests.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EnforcementReport {
    allowed_syscalls: usize,
    program_instructions: usize,
    scope: Scope,
    violation: ViolationAction,
}

impl EnforcementReport {
    // Only the Linux backend can produce one, because only the Linux backend can
    // install a filter. Constructible everywhere it would be a type whose
    // existence claims enforcement that never happened.
    #[cfg(target_os = "linux")]
    pub(crate) const fn new(
        allowed_syscalls: usize,
        program_instructions: usize,
        scope: Scope,
        violation: ViolationAction,
    ) -> Self {
        Self {
            allowed_syscalls,
            program_instructions,
            scope,
            violation,
        }
    }
    /// How many system calls the installed filter admits.
    #[must_use]
    pub const fn allowed_syscalls(self) -> usize {
        self.allowed_syscalls
    }
    /// Size of the compiled BPF program, in instructions.
    ///
    /// Worth recording: the kernel rejects programs at `BPF_MAXINSNS`, and a
    /// policy quietly approaching that ceiling is a policy about to start
    /// failing installation on some hosts and not others.
    #[must_use]
    pub const fn program_instructions(self) -> usize {
        self.program_instructions
    }
    /// Which threads the filter governs.
    #[must_use]
    pub const fn scope(self) -> Scope {
        self.scope
    }
    /// What the kernel does on a denied call.
    #[must_use]
    pub const fn violation(self) -> ViolationAction {
        self.violation
    }
}
