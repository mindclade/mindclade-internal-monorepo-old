// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Thin deployment lifecycle; execution state lives in worker_runtime.

use mindclade_faults::FaultResult;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Lifecycle {
    Starting,
    Ready,
    Draining,
    Stopped,
}

impl Lifecycle {
    #[must_use]
    pub fn can_accept(self) -> bool {
        self == Self::Ready
    }
    pub fn drain(&mut self) -> FaultResult<()> {
        if *self == Self::Ready {
            *self = Self::Draining;
        }
        Ok(())
    }
}
