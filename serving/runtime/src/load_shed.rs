// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic local load shedding based on explicit resource signals.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LoadShedDecision {
    Admit,
    RejectConcurrency,
    RejectQueue,
    RejectMemoryPressure,
    Drain,
}

#[derive(Clone, Copy, Debug)]
pub struct LoadShedder {
    pub maximum_active: u32,
    pub maximum_queued: u32,
    pub memory_pressure_permyriad: u16,
}

impl LoadShedder {
    #[must_use]
    pub fn decide(
        &self,
        active: u32,
        queued: u32,
        memory_pressure: u16,
        draining: bool,
    ) -> LoadShedDecision {
        if draining {
            return LoadShedDecision::Drain;
        }
        if active >= self.maximum_active {
            return LoadShedDecision::RejectConcurrency;
        }
        if queued >= self.maximum_queued {
            return LoadShedDecision::RejectQueue;
        }
        if memory_pressure >= self.memory_pressure_permyriad {
            return LoadShedDecision::RejectMemoryPressure;
        }
        LoadShedDecision::Admit
    }
}
