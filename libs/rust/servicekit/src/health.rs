//! Health and readiness registry.

use mindclade_runtime_core::Clock;
use mindclade_faults::{
    Fault, FaultResult
};
use std::collections::BTreeMap;
use std::sync::{
    Arc, RwLock
};
use std::time::SystemTime;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HealthStatus {
    Healthy, Degraded, Unhealthy, Starting
}

impl HealthStatus {
    #[must_use] pub const fn is_ready(self) -> bool {
        matches!(self, Self::Healthy | Self::Degraded)
    }
}

#[derive(Clone, Debug)]
pub struct HealthReport {
    pub status: HealthStatus,
    pub message: String,
    pub updated_at: SystemTime
}

#[derive(Clone)]
pub struct HealthRegistry {
    clock: Arc<dyn Clock>, reports: Arc<RwLock<BTreeMap<String, HealthReport>>>
}

impl HealthRegistry {
    #[must_use] pub fn new(clock: Arc<dyn Clock>) -> Self {
        Self {
            clock, reports: Arc::new(RwLock::new(BTreeMap::new()))
        }
    }
    pub fn set(&self, component: impl Into<String>, status: HealthStatus, message: impl Into<String>) -> FaultResult<()> {
        let component = component.into();
        let message = message.into();
        if component.is_empty() || component.len() > 128 || message.len() > 1024 {
            return Err(Fault::invalid_argument("health report fields are invalid"));
        }
        self.reports.write().unwrap_or_else(|poisoned| poisoned.into_inner()).insert(component, HealthReport {
            status, message, updated_at: self.clock.system_now()
        });
        Ok(())
    }
    #[must_use] pub fn snapshot(&self) -> BTreeMap<String, HealthReport> {
        self.reports.read().unwrap_or_else(|poisoned| poisoned.into_inner()).clone()
    }
    #[must_use] pub fn is_live(&self) -> bool {
        !self.reports.read().unwrap_or_else(|poisoned| poisoned.into_inner()).values().any(|report| report.status == HealthStatus::Unhealthy)
    }
    #[must_use] pub fn is_ready(&self) -> bool {
        let guard = self.reports.read().unwrap_or_else(|poisoned| poisoned.into_inner());
        !guard.is_empty() && guard.values().all(|report| report.status.is_ready())
    }
}
