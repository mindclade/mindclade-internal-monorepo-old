// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lease identity tying execution authority to a fencing generation.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::FencingToken;
use mindclade_worker_protocol::ResourceId;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LeaseIdentity {
    pub ticket_id: ResourceId,
    pub fencing_token: FencingToken,
    pub expires_unix_millis: u64,
}

impl LeaseIdentity {
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        if self.ticket_id.kind() != "ticket" {
            return Err(Fault::invalid_argument(
                "worker lease must reference a ticket resource",
            ));
        }
        if self.expires_unix_millis <= now_unix_millis {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "worker lease has expired",
            ));
        }
        Ok(())
    }
}
