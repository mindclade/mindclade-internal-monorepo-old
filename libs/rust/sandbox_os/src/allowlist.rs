// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded, deduplicated syscall allowlists.
//!
//! Entries are carried as names rather than numbers so the type compiles on
//! every supported host. Numbers are architecture-specific and are resolved
//! only inside the Linux backend, immediately before the filter is compiled. A
//! name the backend cannot resolve is a hard error there, never a silently
//! dropped rule: a dropped rule would quietly widen or narrow the filter, and
//! both directions are defects.

use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeSet;

/// One admitted system call, identified by its kernel name.
///
/// The name is the stable identity across architectures; `openat` is 257 on
/// `x86_64` and 56 on aarch64, and only the backend knows which host it is
/// on.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct Syscall(&'static str);

impl Syscall {
    /// Names a system call to admit.
    #[must_use]
    pub const fn new(name: &'static str) -> Self {
        Self(name)
    }
    /// Returns the kernel name.
    #[must_use]
    pub const fn name(self) -> &'static str {
        self.0
    }
}

/// Declares a syscall allowlist set from bare kernel names.
///
/// Modelled on the `allow_list!` macro in Cloudflare's `foundations`, which
/// ADR-0026 evaluated and declined to depend on. Expands to an array literal so
/// a set can be a `static`.
#[macro_export]
macro_rules! allow_list {
    ($($name:ident),* $(,)?) => {
        [$($crate::Syscall::new(stringify!($name))),*]
    };
}

/// A bounded set of admitted system calls.
///
/// Deduplicated and ordered so that two policies built from the same sets in a
/// different order compile to byte-identical filters, which is what makes a
/// filter reviewable and a regression diffable.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct SyscallAllowList {
    names: BTreeSet<&'static str>,
}

impl SyscallAllowList {
    /// Largest admissible allowlist.
    ///
    /// Linux defines fewer than 500 system calls, so this is roughly four times
    /// the whole table and no real policy approaches it. It exists because an
    /// unbounded accumulator is exactly the shape this repository refuses to
    /// ship, and because a policy that has grown past every syscall the kernel
    /// has is not a confinement policy any more.
    pub const MAXIMUM_ENTRIES: usize = 2048;

    /// Creates an empty allowlist.
    #[must_use]
    pub fn new() -> Self {
        Self {
            names: BTreeSet::new(),
        }
    }
    /// Admits every syscall in `syscalls`, rejecting growth past the bound.
    pub fn allowing(mut self, syscalls: &[Syscall]) -> FaultResult<Self> {
        for syscall in syscalls {
            if self.names.len() >= Self::MAXIMUM_ENTRIES && !self.names.contains(syscall.name()) {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "syscall allowlist exceeds the admissible bound",
                )
                .with_context("maximum_entries", Self::maximum_entries_context())
                .with_context("rejected_syscall", syscall.name()));
            }
            self.names.insert(syscall.name());
        }
        Ok(self)
    }
    /// Number of admitted system calls.
    #[must_use]
    pub fn len(&self) -> usize {
        self.names.len()
    }
    /// Whether nothing is admitted.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.names.is_empty()
    }
    /// Whether a specific system call is admitted.
    #[must_use]
    pub fn contains(&self, syscall: Syscall) -> bool {
        self.names.contains(syscall.name())
    }
    /// Iterates admitted names in deterministic order.
    pub fn names(&self) -> impl Iterator<Item = &'static str> + '_ {
        self.names.iter().copied()
    }

    fn maximum_entries_context() -> u64 {
        u64::try_from(Self::MAXIMUM_ENTRIES).unwrap_or(u64::MAX)
    }
}
