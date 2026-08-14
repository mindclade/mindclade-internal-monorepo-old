//! Ingestion-stage worker limits.

use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct IngestionWorkerConfig {
    pub maximum_outputs: usize,
}

impl IngestionWorkerConfig {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_outputs == 0 || self.maximum_outputs > 4_096 {
            return Err(Fault::invalid_argument("ingestion worker output limit is invalid"));
        }
        Ok(())
    }
}
