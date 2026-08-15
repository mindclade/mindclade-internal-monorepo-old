// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Monotonic deadlines with injected clocks.
#![forbid(unsafe_code)]

use crate::Clock;
use mindclade_faults::{Code, Fault, FaultResult};
use std::time::{Duration, Instant};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Deadline(Instant);

impl Deadline {
    #[must_use]
    pub const fn at(instant: Instant) -> Self {
        Self(instant)
    }
    pub fn after(clock: &dyn Clock, duration: Duration) -> FaultResult<Self> {
        let now = clock.monotonic_now();
        let instant = now.checked_add(duration).ok_or_else(|| {
            Fault::new(Code::OutOfRange, "deadline exceeds monotonic clock domain")
        })?;
        Ok(Self(instant))
    }
    #[must_use]
    pub fn is_expired(self, clock: &dyn Clock) -> bool {
        clock.monotonic_now() >= self.0
    }
    /// Remaining duration. Returning zero for an already-expired deadline is
    /// intentional and does not hide an accounting invariant.
    #[must_use]
    pub fn remaining(self, clock: &dyn Clock) -> Duration {
        self.0.saturating_duration_since(clock.monotonic_now())
    }
    #[must_use]
    pub const fn instant(self) -> Instant {
        self.0
    }
}
