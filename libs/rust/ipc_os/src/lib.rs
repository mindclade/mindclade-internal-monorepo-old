// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! OS-specific bulk IPC leaf adapters.
//!
//! Control messages remain in `mindclade_ipc`; this crate owns only the
//! operating-system mechanism used by large local buffers.
#![allow(clippy::missing_errors_doc)]

pub mod broker;
pub mod file;

#[cfg(target_os = "linux")]
mod linux;
pub use broker::{BulkBackend, BulkBufferBroker};
#[cfg(target_os = "linux")]
pub use linux::MemfdSegment;
use mindclade_faults::FaultResult;
#[cfg(not(target_os = "linux"))]
use mindclade_faults::{Code, Fault};
use mindclade_worker_protocol::BufferDescriptor;

/// Handle retained by the runtime host until every consumer releases a bulk
/// descriptor.  Implementations close/remove the underlying OS object on drop.
pub trait BulkSegment: Send + Sync {
    fn descriptor(&self) -> &BufferDescriptor;
    fn read_verified(&self, maximum_bytes: u64, now_unix_millis: u64) -> FaultResult<Vec<u8>>;
}

#[cfg(not(target_os = "linux"))]
#[derive(Debug)]
pub struct MemfdSegment;

#[cfg(not(target_os = "linux"))]
impl MemfdSegment {
    pub fn create(
        _name: &str,
        _bytes: &[u8],
        _generation: u64,
        _owner_process: &str,
        _lease_expires_unix_millis: u64,
        _now_unix_millis: u64,
    ) -> FaultResult<Self> {
        Err(Fault::new(
            Code::Unimplemented,
            "memfd bulk IPC requires Linux",
        ))
    }
}
