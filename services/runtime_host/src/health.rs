// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Runtime-host readiness state.

use std::sync::atomic::{AtomicBool, Ordering};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HostHealthSnapshot {
    pub accepting: bool,
    pub process_supervisor_ready: bool,
    pub gpu_ready: bool,
}

impl HostHealthSnapshot {
    #[must_use]
    pub const fn live(self) -> bool {
        true
    }
    #[must_use]
    pub const fn ready(self) -> bool {
        self.accepting && self.process_supervisor_ready && self.gpu_ready
    }
}

#[derive(Debug)]
pub struct HostHealth {
    accepting: AtomicBool,
    process_supervisor_ready: AtomicBool,
    gpu_ready: AtomicBool,
}

impl HostHealth {
    #[must_use]
    pub fn new() -> Self {
        Self {
            accepting: AtomicBool::new(false),
            process_supervisor_ready: AtomicBool::new(false),
            gpu_ready: AtomicBool::new(false),
        }
    }
    pub fn set_accepting(&self, v: bool) {
        self.accepting.store(v, Ordering::Release);
    }
    pub fn set_process_supervisor_ready(&self, v: bool) {
        self.process_supervisor_ready.store(v, Ordering::Release);
    }
    pub fn set_gpu_ready(&self, v: bool) {
        self.gpu_ready.store(v, Ordering::Release);
    }
    #[must_use]
    pub fn snapshot(&self) -> HostHealthSnapshot {
        HostHealthSnapshot {
            accepting: self.accepting.load(Ordering::Acquire),
            process_supervisor_ready: self.process_supervisor_ready.load(Ordering::Acquire),
            gpu_ready: self.gpu_ready.load(Ordering::Acquire),
        }
    }
}

impl Default for HostHealth {
    fn default() -> Self {
        Self::new()
    }
}
