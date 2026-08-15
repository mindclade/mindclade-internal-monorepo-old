// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Worker heartbeat freshness checks.
#![forbid(unsafe_code)]

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Heartbeat {
    pub sequence: u64,
    pub observed_unix_millis: u64,
    pub progress_sequence: u64,
}

impl Heartbeat {
    /// Return whether this heartbeat is older than the configured maximum age.
    /// Future observations are rejected instead of being silently clamped to
    /// age zero, because a clock-domain mismatch must not look healthy.
    pub fn is_stale(self, now: u64, maximum_age_millis: u64) -> FaultResult<bool> {
        if self.sequence == 0 {
            return Err(Fault::invalid_argument(
                "heartbeat sequence must be non-zero",
            ));
        }
        let age = now.checked_sub(self.observed_unix_millis).ok_or_else(|| {
            Fault::new(
                Code::FailedPrecondition,
                "heartbeat observation is in the future",
            )
            .with_context("now_unix_millis", now)
            .with_context("observed_unix_millis", self.observed_unix_millis)
        })?;
        Ok(age > maximum_age_millis)
    }
}
