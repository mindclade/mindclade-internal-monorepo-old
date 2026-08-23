// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Kernel-enforced syscall confinement for the worker process boundary.
//!
//! seccomp filters are process-scoped and irreversible, so this is the only
//! layer that can install one: a parser crate cannot, because it does not own
//! the process, and installing per-parse would be both impossible and pointless.
//! [`WorkerRuntime::start_confined`] is therefore where confinement happens —
//! at the transition from `Starting` to `Ready`, before any ticket is leased and
//! therefore before any untrusted model input or scientific file is opened.
//!
//! The failure semantics are the whole point. A worker that asked for
//! confinement and did not get it does not become `Ready`; it goes to `Failed`
//! and the fault propagates to the composition root. The operator's belief that
//! the worker is confined stays true or the worker stops.

use mindclade_faults::FaultResult;
use mindclade_sandbox_os::{SandboxPolicy, SandboxRequest, profiles};

/// Confinement for a worker that reads untrusted model inputs and scientific
/// files over inherited descriptors and the local filesystem.
///
/// This is the default for a stage worker. It denies `execve`, `ptrace`,
/// `socket`, `prctl`, `mount`, `bpf` and everything else outside
/// `mindclade_sandbox_os::profiles`, so the blast radius of a wrong bound in
/// `bounded_parse` or `bio_formats` stops at this process.
pub fn untrusted_input_policy() -> FaultResult<SandboxPolicy> {
    profiles::untrusted_input_worker()
}

/// Confinement for a worker that additionally opens its own sockets, rather
/// than inheriting an already-connected transport from its supervisor.
///
/// Prefer [`untrusted_input_policy`] where the supervisor can pass descriptors:
/// a worker that cannot call `socket` cannot be turned into an exfiltration
/// channel by a parser defect, and no amount of code review gives that
/// guarantee as cheaply.
pub fn networked_policy() -> FaultResult<SandboxPolicy> {
    profiles::networked_worker()
}

/// The request a worker composition root passes to
/// [`WorkerRuntime::start_confined`] when it wants the default worker policy.
///
/// [`WorkerRuntime::start_confined`]: crate::WorkerRuntime::start_confined
pub fn untrusted_input_request() -> FaultResult<SandboxRequest> {
    Ok(SandboxRequest::Required(untrusted_input_policy()?))
}

/// The request for a worker that owns its own transport.
pub fn networked_request() -> FaultResult<SandboxRequest> {
    Ok(SandboxRequest::Required(networked_policy()?))
}
