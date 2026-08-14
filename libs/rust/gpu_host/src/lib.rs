//! GPU-host admission primitives without a CUDA/ROCm API dependency.
#![forbid(unsafe_code)]

pub mod budget;
pub mod device;
pub mod inventory;
pub mod memory;
pub mod process;
pub mod providers;
pub use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{Budget, Reservation, ResourceKind, ResourceVector};
use std::sync::Arc;

const MAX_VENDOR_BYTES: usize = 32;
const MAX_ARCHITECTURE_BYTES: usize = 64;

fn valid_token(value: &str, maximum_bytes: usize) -> bool {
    !value.is_empty()
        && value.len() <= maximum_bytes
        && value == value.trim()
        && value.bytes().all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeviceCapability {
    pub vendor: String,
    pub architecture: String,
    pub total_memory_bytes: u64,
}
impl DeviceCapability {
    pub fn validate(&self) -> FaultResult<()> {
        if !valid_token(&self.vendor, MAX_VENDOR_BYTES)
            || !valid_token(&self.architecture, MAX_ARCHITECTURE_BYTES)
            || self.total_memory_bytes == 0
        {
            return Err(Fault::invalid_argument("GPU capability is invalid"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModelSlotRequest {
    pub model_digest: Digest,
    pub minimum_memory_bytes: u64,
    pub pinned_memory_bytes: u64,
}
impl ModelSlotRequest {
    pub fn validate(&self) -> FaultResult<()> {
        if self.model_digest == Digest::ZERO || self.minimum_memory_bytes == 0 {
            return Err(Fault::invalid_argument("model slot request is invalid"));
        }
        Ok(())
    }
}

pub struct ModelSlot {
    _reservation: Reservation,
    pub request: ModelSlotRequest,
}

pub struct GpuHost {
    capability: DeviceCapability,
    budget: Arc<Budget>,
}
impl GpuHost {
    pub fn new(capability: DeviceCapability, budget: Arc<Budget>) -> FaultResult<Self> {
        capability.validate()?;
        Ok(Self { capability, budget })
    }
    #[must_use]
    pub fn capability(&self) -> &DeviceCapability {
        &self.capability
    }
    #[must_use]
    pub fn budget(&self) -> &Arc<Budget> {
        &self.budget
    }
    pub fn reserve_model(&self, request: ModelSlotRequest) -> FaultResult<ModelSlot> {
        request.validate()?;
        if request.minimum_memory_bytes > self.capability.total_memory_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "model does not fit physical GPU memory",
            ));
        }
        let resources = ResourceVector::new()
            .set(ResourceKind::GpuMemoryEstimateBytes, request.minimum_memory_bytes)
            .set(ResourceKind::PinnedMemoryBytes, request.pinned_memory_bytes);
        Ok(ModelSlot {
            _reservation: self.budget.reserve(resources)?,
            request,
        })
    }
}
