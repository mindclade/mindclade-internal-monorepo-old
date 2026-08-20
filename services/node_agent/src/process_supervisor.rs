// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded child-process registry and cleanup.
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;
use std::process::Child;
use std::sync::Mutex;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ManagedProcess {
    pub pid: u32,
}

#[derive(Debug)]
pub struct ProcessSupervisor {
    maximum: u32,
    pub(crate) children: Mutex<BTreeMap<u32, Child>>,
}

impl ProcessSupervisor {
    pub fn new(maximum: u32) -> FaultResult<Self> {
        if maximum == 0 {
            return Err(Fault::invalid_argument(
                "child process limit must be positive",
            ));
        }
        Ok(Self {
            maximum,
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
            let _ = child.kill();
            child.wait().map_err(|error| {
                Fault::new(
                    Code::Unavailable,
                    "child limit reached and rejected process could not be reaped",
                )
                .with_source(error)
            })?;
            return Err(Fault::new(
                Code::ResourceExhausted,
                "child process limit reached",
            ));
        }
        let pid = child.id();
        children.insert(pid, child);
        Ok(ManagedProcess { pid })
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
        let _ = child.kill();
        child.wait().map(|_| ()).map_err(|error| {
            Fault::new(Code::Unavailable, "failed to wait for child process").with_source(error)
        })
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
