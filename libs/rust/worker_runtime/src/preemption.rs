// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded preemption notice used to enter a safe drain path.

use mindclade_faults::{Code, Fault, FaultResult};

const MAX_REASON_BYTES: usize = 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PreemptionNotice {
    pub reason: String,
    pub deadline_unix_millis: u64,
}

impl PreemptionNotice {
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        let reason = self.reason.trim();
        if reason.is_empty() || reason != self.reason || reason.len() > MAX_REASON_BYTES {
            return Err(Fault::invalid_argument("preemption reason is invalid"));
        }
        if self.deadline_unix_millis <= now_unix_millis {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "preemption deadline has already expired",
            ));
        }
        Ok(())
    }
}
