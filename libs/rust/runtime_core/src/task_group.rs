// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Owned task groups. Production Tokio services adapt the same ownership law
//! to `JoinSet`; detached tasks are forbidden.

use crate::CancellationToken;
use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Mutex;
use std::thread::{self, JoinHandle};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TaskFailure {
    pub name: String,
    pub message: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct TaskReport {
    pub joined: usize,
    pub failures: Vec<TaskFailure>,
}
impl TaskReport {
    #[must_use]
    pub fn is_clean(&self) -> bool {
        self.failures.is_empty()
    }
}

#[derive(Debug)]
pub struct TaskGroup {
    cancellation: CancellationToken,
    handles: Mutex<Vec<(String, JoinHandle<FaultResult<()>>)>>,
}
impl TaskGroup {
    #[must_use]
    pub fn new(cancellation: CancellationToken) -> Self {
        Self {
            cancellation,
            handles: Mutex::new(Vec::new()),
        }
    }
    #[must_use]
    pub fn cancellation(&self) -> CancellationToken {
        self.cancellation.clone()
    }
    pub fn spawn<F>(&self, name: impl Into<String>, task: F) -> FaultResult<()>
    where
        F: FnOnce(CancellationToken) -> FaultResult<()> + Send + 'static,
    {
        let name = name.into();
        if name.trim().is_empty() || name.len() > 128 {
            return Err(Fault::invalid_argument("task name is invalid"));
        }
        if self.cancellation.is_cancelled() {
            return Err(Fault::new(Code::Cancelled, "task group is cancelled"));
        }
        let token = self.cancellation.clone();
        let handle = thread::Builder::new()
            .name(name.clone())
            .spawn(move || task(token))
            .map_err(|e| {
                Fault::new(Code::ResourceExhausted, "failed to spawn owned task").with_source(e)
            })?;
        self.handles
            .lock()
            .unwrap_or_else(|p| p.into_inner())
            .push((name, handle));
        Ok(())
    }
    pub fn cancel(&self, reason: impl Into<String>) -> bool {
        self.cancellation.cancel(reason)
    }
    pub fn join_all(&self) -> TaskReport {
        let pending = {
            let mut h = self.handles.lock().unwrap_or_else(|p| p.into_inner());
            std::mem::take(&mut *h)
        };
        let mut report = TaskReport::default();
        for (name, handle) in pending {
            report.joined += 1;
            match handle.join() {
                Ok(Ok(())) => {}
                Ok(Err(error)) => report.failures.push(TaskFailure {
                    name,
                    message: error.to_string(),
                }),
                Err(_) => report.failures.push(TaskFailure {
                    name,
                    message: "task panicked".to_owned(),
                }),
            }
        }
        report
    }
}
