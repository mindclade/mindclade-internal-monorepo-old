// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_worker_protocol::{BufferAccess, BufferDescriptor, BufferTransport};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SharedMemoryDescriptor {
    pub descriptor: BufferDescriptor,
}
impl SharedMemoryDescriptor {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        segment_id: String,
        generation: u64,
        length: u64,
        digest: Digest,
        owner_process: String,
        lease_expires_unix_millis: u64,
        locator: String,
        now_unix_millis: u64,
    ) -> FaultResult<Self> {
        if locator.contains('\0') {
            return Err(Fault::invalid_argument(
                "shared memory locator contains NUL",
            ));
        }
        let descriptor = BufferDescriptor {
            segment_id,
            generation,
            range: ByteRange::new(0, length)?,
            element_type: "bytes".into(),
            shape: vec![length],
            digest,
            owner_process,
            lease_expires_unix_millis,
            access: BufferAccess::ReadWrite,
            transport: BufferTransport::SharedMemory,
            locator,
        };
        descriptor.validate(now_unix_millis)?;
        Ok(Self { descriptor })
    }
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        if self.descriptor.transport != BufferTransport::SharedMemory {
            return Err(Fault::invalid_argument(
                "descriptor is not shared-memory transport",
            ));
        }
        self.descriptor.validate(now_unix_millis)
    }
}
