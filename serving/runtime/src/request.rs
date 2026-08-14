//! Model-neutral online inference request envelope.
use crate::AdmissionRequest;
use mindclade_faults::{
    Fault, FaultResult
};
use mindclade_identifiers::ResourceId;
use mindclade_worker_protocol::AdmissionGrant;

#[derive(Clone, Debug)]
pub struct InferenceRequest {
    pub request_id: ResourceId,
    pub grant: AdmissionGrant,
    pub admission: AdmissionRequest,
    pub payload_descriptor: Option<String>,
}

impl InferenceRequest {
    pub fn validate(&self, maximum_key_bytes: usize) -> FaultResult<()> {
        if self.request_id.kind() != "request" {
            return Err(Fault::invalid_argument("inference request id has wrong kind"));
        }
        self.admission.validate(maximum_key_bytes)?;
        if self.payload_descriptor.as_ref().is_some_and(|value| value.is_empty() || value.len() > 4096) {
            return Err(Fault::invalid_argument("inference payload descriptor is invalid"));
        }
        Ok(())
    }
}
