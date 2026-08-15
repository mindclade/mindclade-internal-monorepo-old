// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Cooperative cancellation shared by runtime, worker, and transfer code.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, Default)]
pub struct CancellationToken {
    inner: Arc<Inner>,
}

#[derive(Debug, Default)]
struct Inner {
    cancelled: AtomicBool,
    reason: Mutex<Option<String>>,
}

impl CancellationToken {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
    pub fn cancel(&self, reason: impl Into<String>) -> bool {
        let first = !self.inner.cancelled.swap(true, Ordering::AcqRel);
        if first {
            *self.inner.reason.lock().unwrap_or_else(|p| p.into_inner()) = Some(reason.into());
        }
        first
    }
    #[must_use]
    pub fn is_cancelled(&self) -> bool {
        self.inner.cancelled.load(Ordering::Acquire)
    }
    #[must_use]
    pub fn reason(&self) -> Option<String> {
        self.inner
            .reason
            .lock()
            .unwrap_or_else(|p| p.into_inner())
            .clone()
    }
}
