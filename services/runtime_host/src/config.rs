//! Runtime-host configuration with explicit hard limits.

use mindclade_faults::{Fault, FaultResult};
use mindclade_ipc::MAX_CONTROL_PAYLOAD;
use mindclade_runtime_core::{ResourceKind, ResourceVector};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HostConfig {
    pub maximum_processes: u32,
    pub maximum_model_slots: u32,
    pub maximum_input_buffers: usize,
    pub maximum_control_payload_bytes: u64,
    pub node_resources: ResourceVector,
}

impl HostConfig {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_processes == 0
            || self.maximum_model_slots == 0
            || self.maximum_model_slots > self.maximum_processes
            || self.maximum_input_buffers == 0
            || self.maximum_input_buffers > 4_096
            || self.maximum_control_payload_bytes == 0
            || self.maximum_control_payload_bytes > MAX_CONTROL_PAYLOAD.get()
            || self.node_resources.get(ResourceKind::ResidentMemoryBytes) == 0
            || self.node_resources.get(ResourceKind::OpenFileDescriptors) == 0
            || self.node_resources.get(ResourceKind::Processes) == 0
            || self.node_resources.get(ResourceKind::CpuThreads) == 0
        {
            return Err(Fault::invalid_argument("runtime host configuration is invalid"));
        }
        Ok(())
    }
}
