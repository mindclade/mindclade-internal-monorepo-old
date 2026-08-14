use mindclade_runtime_core::ManualClock;
use mindclade_telemetry::MemorySink;
use mindclade_servicekit::{HealthRegistry, HealthStatus, ShutdownToken, Supervisor};
use std::sync::Arc;
use std::time::{Instant, SystemTime};

#[test]
fn readiness_and_shutdown_are_explicit() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let health = HealthRegistry::new(clock.clone());
    assert!(!health.is_ready()); assert!(health.set("artifact-store", HealthStatus::Healthy, "ready").is_ok()); assert!(health.is_ready());
    let token = ShutdownToken::new(); let mut supervisor = Supervisor::new(token.clone(), clock, Arc::new(MemorySink::default()));
    assert!(supervisor.spawn("worker", |shutdown| { shutdown.wait_timeout(std::time::Duration::from_millis(1)); Ok(()) }).is_ok());
    supervisor.shutdown(); assert!(supervisor.join().is_empty()); assert!(token.is_cancelled());
}

use mindclade_faults::{Fault, FaultResult};
use mindclade_servicekit::{Component, LifecycleState, Service};
use std::sync::Mutex;

#[derive(Debug)]
struct RecordingComponent {
    name: &'static str,
    events: Arc<Mutex<Vec<String>>>,
    fail_start: bool,
    fail_drain: bool,
    fail_stop: bool,
}

impl RecordingComponent {
    fn record(&self, action: &str) {
        self.events
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .push(format!("{}:{action}", self.name));
    }
}

impl Component for RecordingComponent {
    fn name(&self) -> &str {
        self.name
    }
    fn start(&mut self) -> FaultResult<()> {
        self.record("start");
        if self.fail_start {
            return Err(Fault::internal("start failed"));
        }
        Ok(())
    }
    fn drain(&mut self) -> FaultResult<()> {
        self.record("drain");
        if self.fail_drain {
            return Err(Fault::internal("drain failed"));
        }
        Ok(())
    }
    fn stop(&mut self) -> FaultResult<()> {
        self.record("stop");
        if self.fail_stop {
            return Err(Fault::internal("stop failed"));
        }
        Ok(())
    }
}

fn component(
    name: &'static str,
    events: Arc<Mutex<Vec<String>>>,
    fail_start: bool,
    fail_drain: bool,
    fail_stop: bool,
) -> Box<dyn Component> {
    Box::new(RecordingComponent {
        name,
        events,
        fail_start,
        fail_drain,
        fail_stop,
    })
}

#[test]
fn startup_failure_rolls_back_started_components() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut service = Service::new();
    service
        .register(component("database", events.clone(), false, false, false))
        .unwrap();
    service
        .register(component("server", events.clone(), true, false, false))
        .unwrap();
    assert!(service.start().is_err());
    assert_eq!(service.lifecycle().state(), LifecycleState::Failed);
    assert_eq!(
        events
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .as_slice(),
        ["database:start", "server:start", "database:stop"]
    );
}

#[test]
fn drain_and_stop_are_reverse_order_and_exhaustive() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut service = Service::new();
    service
        .register(component("first", events.clone(), false, false, true))
        .unwrap();
    service
        .register(component("second", events.clone(), false, true, false))
        .unwrap();
    service.start().unwrap();
    assert!(service.drain().is_err());
    assert!(service.stop().is_err());
    assert_eq!(service.lifecycle().state(), LifecycleState::Failed);
    assert_eq!(
        events
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .as_slice(),
        [
            "first:start",
            "second:start",
            "second:drain",
            "first:drain",
            "second:stop",
            "first:stop",
        ]
    );
}
