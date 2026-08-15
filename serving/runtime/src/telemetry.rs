// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Reusable serving counters with bounded names.

use mindclade_telemetry::CounterRegistry;
use std::collections::BTreeMap;

#[derive(Clone, Debug, Default)]
pub struct ServingMetrics {
    counters: CounterRegistry,
}

impl ServingMetrics {
    pub fn admitted(&self) {
        let _ = self.counters.add("serving.admitted", 1);
    }
    pub fn rejected(&self) {
        let _ = self.counters.add("serving.rejected", 1);
    }
    pub fn completed(&self) {
        let _ = self.counters.add("serving.completed", 1);
    }
    #[must_use]
    pub fn snapshot(&self) -> BTreeMap<String, u64> {
        self.counters.snapshot()
    }
}
