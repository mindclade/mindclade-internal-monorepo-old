// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Cooperative shutdown token.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;

#[derive(Debug)]
struct State {
    cancelled: AtomicBool,
    mutex: Mutex<()>,
    changed: Condvar,
}

#[derive(Clone, Debug)]
pub struct ShutdownToken(Arc<State>);

impl Default for ShutdownToken {
    fn default() -> Self {
        Self::new()
    }
}

impl ShutdownToken {
    #[must_use]
    pub fn new() -> Self {
        Self(Arc::new(State {
            cancelled: AtomicBool::new(false),
            mutex: Mutex::new(()),
            changed: Condvar::new(),
        }))
    }
    #[must_use]
    pub fn cancel(&self) -> bool {
        let changed = !self.0.cancelled.swap(true, Ordering::AcqRel);
        if changed {
            self.0.changed.notify_all();
        }
        changed
    }
    #[must_use]
    pub fn is_cancelled(&self) -> bool {
        self.0.cancelled.load(Ordering::Acquire)
    }
    #[must_use]
    pub fn wait_timeout(&self, timeout: Duration) -> bool {
        if self.is_cancelled() {
            return true;
        }
        let guard = self
            .0
            .mutex
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let _result = self
            .0
            .changed
            .wait_timeout_while(guard, timeout, |()| !self.is_cancelled());
        self.is_cancelled()
    }
}
