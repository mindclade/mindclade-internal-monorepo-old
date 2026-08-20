// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded child-process registry and cleanup.
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_process_os::{
    DEFAULT_TERMINATION_GRACE, MAXIMUM_TERMINATION_GRACE, terminate_process_group,
};
use std::collections::BTreeMap;
use std::process::{Child, ExitStatus};
use std::sync::Mutex;
use std::time::Duration;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ManagedProcess {
    pub pid: u32,
}

#[derive(Debug)]
pub struct ProcessSupervisor {
    maximum: u32,
    termination_grace: Duration,
    children: Mutex<BTreeMap<u32, Child>>,
}

impl ProcessSupervisor {
    pub fn new(maximum: u32) -> FaultResult<Self> {
        Self::with_termination_grace(maximum, DEFAULT_TERMINATION_GRACE)
    }

    pub fn with_termination_grace(maximum: u32, termination_grace: Duration) -> FaultResult<Self> {
        if maximum == 0 {
            return Err(Fault::invalid_argument(
                "child process limit must be positive",
            ));
        }
        if termination_grace > MAXIMUM_TERMINATION_GRACE {
            return Err(Fault::invalid_argument(
                "child termination grace exceeds supported bound",
            ));
        }
        Ok(Self {
            maximum,
            termination_grace,
            children: Mutex::new(BTreeMap::new()),
        })
    }
    pub fn register(&self, mut child: Child) -> FaultResult<ManagedProcess> {
        let mut children = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let maximum = usize::try_from(self.maximum).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "child process limit exceeds platform usize",
            )
        })?;
        if children.len() >= maximum {
            // `register` consumes ownership. A rejected child must therefore
            // be terminated and reaped here rather than being dropped alive.
            terminate_process_group(&mut child, self.termination_grace)?;
            return Err(Fault::new(
                Code::ResourceExhausted,
                "child process limit reached",
            ));
        }
        let pid = child.id();
        children.insert(pid, child);
        Ok(ManagedProcess { pid })
    }

    pub(crate) fn try_wait(&self, process: ManagedProcess) -> FaultResult<Option<ExitStatus>> {
        let mut children = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let child = children
            .get_mut(&process.pid)
            .ok_or_else(|| Fault::internal("supervised child disappeared"))?;
        child.try_wait().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to poll supervised child").with_source(error)
        })
    }

    /// Finish a child that has exited and remove any descendants which remain
    /// in its process group (including descendants retaining inherited pipes).
    pub(crate) fn finish(&self, process: ManagedProcess) -> FaultResult<()> {
        self.terminate(process)
    }

    pub fn terminate(&self, process: ManagedProcess) -> FaultResult<()> {
        let Some(mut child) = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .remove(&process.pid)
        else {
            return Ok(());
        };
        terminate_process_group(&mut child, self.termination_grace)
    }
    pub fn terminate_all(&self) -> FaultResult<()> {
        let pids: Vec<u32> = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .keys()
            .copied()
            .collect();
        let mut first_fault = None;
        for pid in pids {
            if let Err(fault) = self.terminate(ManagedProcess { pid })
                && first_fault.is_none()
            {
                first_fault = Some(fault);
            }
        }
        if let Some(fault) = first_fault {
            return Err(fault);
        }
        Ok(())
    }
    #[must_use]
    pub fn active(&self) -> usize {
        self.children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .len()
    }
}
