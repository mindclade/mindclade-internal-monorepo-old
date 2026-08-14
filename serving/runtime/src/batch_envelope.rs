//! Coarse batching compatibility decided by Rust without owning tensor semantics.

use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct BatchCompatibilityKey {
    pub deployment_id: String,
    pub model_bundle: Digest,
    pub execution_class: String,
    pub precision_class: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BatchEnvelope {
    pub request_id: String,
    pub key: BatchCompatibilityKey,
    pub estimated_input_units: u64,
    pub estimated_output_units: u64,
}

impl BatchEnvelope {
    pub fn validate(&self) -> FaultResult<()> {
        if self.request_id.is_empty()
            || self.key.deployment_id.is_empty()
            || self.key.execution_class.is_empty()
            || self.key.precision_class.is_empty()
        {
            return Err(Fault::invalid_argument("batch envelope is invalid"));
        }
        Ok(())
    }
}
