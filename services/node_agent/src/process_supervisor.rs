//! Bounded child-process registry and cleanup.
use mindclade_faults::{
    Code, Fault, FaultResult
};
use std::collections::BTreeMap;
use std::process::Child;
use std::sync::Mutex;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ManagedProcess {
    pub pid: u32
}

#[derive(Debug)]
pub struct ProcessSupervisor {
    maximum: u32, pub(crate) children: Mutex<BTreeMap<u32, Child>>
}

impl ProcessSupervisor {
    pub fn new(maximum: u32) -> FaultResult<Self> {
        if maximum == 0 {
            return Err(Fault::invalid_argument("child process limit must be positive"));
        }
        Ok(Self {
            maximum, children: Mutex::new(BTreeMap::new())
        })
    }
    pub fn register(&self, child: Child) -> FaultResult<ManagedProcess> {
        let mut children = self.children.lock().unwrap_or_else(|p| p.into_inner());
        let maximum = usize::try_from(self.maximum).map_err(|_| Fault::new(Code::OutOfRange, "child process limit exceeds platform usize"))?;
        if children.len() >= maximum {
            return Err(Fault::new(Code::ResourceExhausted, "child process limit reached"));
        }
        let pid = child.id();
        children.insert(pid, child);
        Ok(ManagedProcess {
            pid
        })
    }
    pub fn terminate(&self, process: ManagedProcess) -> FaultResult<()> {
        let Some(mut child) = self.children.lock().unwrap_or_else(|p| p.into_inner()).remove(&process.pid) else {
            return Ok(());
        };
        let _ = child.kill();
        child.wait().map(|_| ()).map_err(|error| Fault::new(Code::Unavailable, "failed to wait for child process").with_source(error))
    }
    pub fn terminate_all(&self) -> FaultResult<()> {
        let pids: Vec<u32> = self.children.lock().unwrap_or_else(|p| p.into_inner()).keys().copied().collect();
        for pid in pids {
            self.terminate(ManagedProcess {
                pid
            })?;
        }
        Ok(())
    }
    #[must_use] pub fn active(&self) -> usize {
        self.children.lock().unwrap_or_else(|p| p.into_inner()).len()
    }
}
