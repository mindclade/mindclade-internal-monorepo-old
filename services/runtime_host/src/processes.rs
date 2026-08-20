// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded process supervision for process-isolated model workers.

use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex};

const MAX_PROCESS_NAME_BYTES: usize = 128;
const MAX_EXECUTABLE_BYTES: usize = 4_096;
const MAX_ARGUMENTS: usize = 256;
const MAX_ARGUMENT_BYTES: usize = 16 * 1024;
const MAX_ENVIRONMENT_ENTRIES: usize = 128;
const MAX_ENVIRONMENT_KEY_BYTES: usize = 128;
const MAX_ENVIRONMENT_VALUE_BYTES: usize = 16 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProcessSpec {
    pub name: String,
    pub executable: String,
    pub arguments: Vec<String>,
    pub environment: BTreeMap<String, String>,
}

impl ProcessSpec {
    pub fn validate(&self) -> FaultResult<()> {
        if self.name.is_empty()
            || self.name.len() > MAX_PROCESS_NAME_BYTES
            || self.name.trim() != self.name
            || self.name.as_bytes().contains(&0)
        {
            return Err(Fault::invalid_argument("worker process name is invalid"));
        }
        if self.executable.is_empty()
            || self.executable.len() > MAX_EXECUTABLE_BYTES
            || self.executable.trim() != self.executable
            || self.executable.as_bytes().contains(&0)
            || !Path::new(&self.executable).is_absolute()
        {
            return Err(Fault::invalid_argument(
                "worker executable must be a bounded absolute path",
            ));
        }
        if self.arguments.len() > MAX_ARGUMENTS
            || self
                .arguments
                .iter()
                .any(|value| value.len() > MAX_ARGUMENT_BYTES || value.as_bytes().contains(&0))
        {
            return Err(Fault::invalid_argument(
                "worker process arguments exceed their bounds",
            ));
        }
        if self.environment.len() > MAX_ENVIRONMENT_ENTRIES
            || self.environment.iter().any(|(key, value)| {
                key.is_empty()
                    || key.len() > MAX_ENVIRONMENT_KEY_BYTES
                    || key.contains('=')
                    || key.as_bytes().contains(&0)
                    || value.len() > MAX_ENVIRONMENT_VALUE_BYTES
                    || value.as_bytes().contains(&0)
            })
        {
            return Err(Fault::invalid_argument(
                "worker process environment exceeds its bounds",
            ));
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct ProcessHandle {
    pub pid: u32,
}

pub trait ProcessLauncher: Send + Sync {
    fn launch(&self, spec: &ProcessSpec) -> FaultResult<ProcessHandle>;
    fn terminate(&self, handle: ProcessHandle) -> FaultResult<()>;
    fn running(&self, handle: ProcessHandle) -> FaultResult<bool>;
}

#[derive(Debug, Default)]
pub struct StdProcessLauncher {
    children: Mutex<BTreeMap<u32, Child>>,
}

impl ProcessLauncher for StdProcessLauncher {
    fn launch(&self, spec: &ProcessSpec) -> FaultResult<ProcessHandle> {
        spec.validate()?;
        let mut command = Command::new(&spec.executable);
        command
            .args(&spec.arguments)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .env_clear();
        for (key, value) in &spec.environment {
            command.env(key, value);
        }
        let child = command.spawn().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to launch model worker process")
                .with_source(error)
        })?;
        let pid = child.id();
        self.children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .insert(pid, child);
        Ok(ProcessHandle { pid })
    }

    fn terminate(&self, handle: ProcessHandle) -> FaultResult<()> {
        let Some(mut child) = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .remove(&handle.pid)
        else {
            return Ok(());
        };

        // `kill` is the force-stop path used after graceful worker drain. The
        // host keeps ownership of the Child until it has been reaped.
        if let Err(error) = child.kill() {
            if let Ok(Some(_)) = child.try_wait() {
                return Ok(());
            }
            self.children
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner)
                .insert(handle.pid, child);
            return Err(Fault::new(
                Code::Unavailable,
                "failed to terminate model worker process",
            )
            .with_source(error));
        }
        child.wait().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to reap model worker process").with_source(error)
        })?;
        Ok(())
    }

    fn running(&self, handle: ProcessHandle) -> FaultResult<bool> {
        let mut children = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let Some(child) = children.get_mut(&handle.pid) else {
            return Ok(false);
        };
        match child.try_wait() {
            Ok(None) => Ok(true),
            Ok(Some(_)) => {
                children.remove(&handle.pid);
                Ok(false)
            }
            Err(error) => Err(Fault::new(
                Code::Unavailable,
                "failed to inspect model worker process",
            )
            .with_source(error)),
        }
    }
}

pub struct ProcessSupervisor {
    launcher: Arc<dyn ProcessLauncher>,
    maximum_processes: u32,
    state: Mutex<SupervisorState>,
}

#[derive(Default)]
struct SupervisorState {
    pending: BTreeSet<String>,
    processes: BTreeMap<String, ProcessHandle>,
}

impl core::fmt::Debug for ProcessSupervisor {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ProcessSupervisor")
            .field("maximum_processes", &self.maximum_processes)
            .field("active", &self.active())
            .finish_non_exhaustive()
    }
}

impl ProcessSupervisor {
    pub fn new(launcher: Arc<dyn ProcessLauncher>, maximum_processes: u32) -> FaultResult<Self> {
        if maximum_processes == 0 {
            return Err(Fault::invalid_argument(
                "maximum process count must be positive",
            ));
        }
        Ok(Self {
            launcher,
            maximum_processes,
            state: Mutex::new(SupervisorState::default()),
        })
    }

    pub fn launch(&self, spec: &ProcessSpec) -> FaultResult<ProcessHandle> {
        spec.validate()?;
        let maximum_processes = usize::try_from(self.maximum_processes).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "worker process limit exceeds platform usize",
            )
        })?;
        {
            let mut state = self
                .state
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner);
            if state.processes.contains_key(&spec.name) || state.pending.contains(&spec.name) {
                return Err(Fault::new(
                    Code::AlreadyExists,
                    "worker process name already exists",
                ));
            }
            let admitted = state
                .processes
                .len()
                .checked_add(state.pending.len())
                .ok_or_else(|| {
                    Fault::new(
                        Code::ResourceExhausted,
                        "worker process accounting overflow",
                    )
                })?;
            if admitted >= maximum_processes {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "worker process limit reached",
                ));
            }
            state.pending.insert(spec.name.clone());
        }

        let launched = self.launcher.launch(spec);
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if !state.pending.remove(&spec.name) {
            return Err(Fault::internal(
                "worker process pending reservation disappeared",
            ));
        }
        let handle = launched?;
        state.processes.insert(spec.name.clone(), handle);
        Ok(handle)
    }

    pub fn stop(&self, name: &str) -> FaultResult<()> {
        let handle = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .processes
            .get(name)
            .copied();
        let Some(handle) = handle else {
            return Ok(());
        };

        self.launcher.terminate(handle)?;
        self.state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .processes
            .remove(name);
        Ok(())
    }

    /// Stop every owned process even if one termination fails.
    ///
    /// The first failure is returned only after all remaining workers have
    /// received their termination attempt.
    pub fn stop_all(&self) -> FaultResult<()> {
        let names: Vec<String> = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .processes
            .keys()
            .cloned()
            .collect();
        let mut first_fault = None;
        for name in names.into_iter().rev() {
            if let Err(fault) = self.stop(&name)
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("process", name));
            }
        }
        first_fault.map_or(Ok(()), Err)
    }

    #[must_use]
    pub fn active(&self) -> usize {
        self.state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .processes
            .len()
    }

    pub fn running(&self, name: &str) -> FaultResult<bool> {
        let handle = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .processes
            .get(name)
            .copied();
        let Some(handle) = handle else {
            return Ok(false);
        };
        if self.launcher.running(handle)? {
            return Ok(true);
        }
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if state.processes.get(name) == Some(&handle) {
            state.processes.remove(name);
        }
        Ok(false)
    }
}
