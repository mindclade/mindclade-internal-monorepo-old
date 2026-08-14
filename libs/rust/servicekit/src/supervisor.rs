//! Named thread supervisor.

use crate::ShutdownToken;
use mindclade_runtime_core::Clock;
use mindclade_faults::{
    Fault, FaultResult
};
use mindclade_telemetry::{
    Event, Severity, Sink
};
use std::sync::Arc;
use std::thread::{
    self, JoinHandle
};

#[derive(Clone, Debug)]
pub struct TaskFailure {
    pub task: String,
    pub fault: Fault
}

pub struct Supervisor {
    shutdown: ShutdownToken, clock: Arc<dyn Clock>, sink: Arc<dyn Sink>, tasks: Vec<(String, JoinHandle<FaultResult<()>>)>
}

impl Supervisor {
    #[must_use] pub fn new(shutdown: ShutdownToken, clock: Arc<dyn Clock>, sink: Arc<dyn Sink>) -> Self {
        Self {
            shutdown, clock, sink, tasks: Vec::new()
        }
    }
    pub fn spawn<F>(&mut self, name: impl Into<String>, task: F) -> FaultResult<()>
    where F: FnOnce(ShutdownToken) -> FaultResult<()> + Send + 'static {
        let name = name.into();
        if name.is_empty() || name.len() > 128 || self.tasks.iter().any(|(current, _)| current == &name) {
            return Err(Fault::invalid_argument("supervised task name is invalid or duplicated"));
        }
        let token = self.shutdown.clone();
        let handle = thread::Builder::new().name(name.clone()).spawn(move || task(token)).map_err(|error| Fault::internal("failed to spawn supervised task")
        .with_source(error))?;
        self.tasks.push((name, handle));
        Ok(())
    }
    pub fn shutdown(&self) {
        self.shutdown.cancel();
    }
    pub fn join(mut self) -> Vec<TaskFailure> {
        self.shutdown.cancel();
        let mut failures = Vec::new();
        for (name, handle) in self.tasks.drain(..) {
            let fault = match handle.join() {
                Ok(Ok(())) => None, Ok(Err(fault)) => Some(fault), Err(_) => Some(Fault::internal("supervised task panicked"))
            };
            if let Some(fault) = fault {
                if let Ok(mut event) = Event::new("service.task_failed", Severity::Error, self.clock.as_ref()) {
                    event.attributes.insert("task", name.clone());
                    event.attributes.insert("code", fault.code().to_string());
                    let _ = self.sink.emit(&event);
                }
                failures.push(TaskFailure {
                    task: name, fault
                });
            }
        }
        let _ = self.sink.flush();
        failures
    }
}
