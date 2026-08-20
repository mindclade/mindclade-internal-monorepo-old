// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-agent limits and resource envelope.
use crate::tool_runner::MAXIMUM_TOOL_OUTPUT_BYTES;
use mindclade_faults::{Fault, FaultResult};
use mindclade_runtime_core::{ResourceKind, ResourceVector};
use std::time::Duration;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NodeAgentConfig {
    pub node_resources: ResourceVector,
    pub maximum_reference_cache_bytes: u64,
    pub maximum_tool_output_bytes: u64,
    pub maximum_children: u32,
    pub tool_poll_interval: Duration,
}
impl NodeAgentConfig {
    #[must_use]
    pub fn maximum_retained_tool_output_bytes(&self) -> u64 {
        self.node_resources.get(ResourceKind::ResidentMemoryBytes) / 4
    }

    pub fn validate(&self) -> FaultResult<()> {
        let required_output_memory = self.maximum_tool_output_bytes.checked_mul(2);
        if self.node_resources.get(ResourceKind::ResidentMemoryBytes) == 0
            || self.node_resources.get(ResourceKind::LocalDiskBytes) == 0
            || self.node_resources.get(ResourceKind::OpenFileDescriptors) == 0
            || self.node_resources.get(ResourceKind::CpuThreads) == 0
            || self.maximum_reference_cache_bytes == 0
            || self.maximum_tool_output_bytes == 0
            || self.maximum_tool_output_bytes > MAXIMUM_TOOL_OUTPUT_BYTES
            || required_output_memory
                .is_none_or(|required| self.maximum_retained_tool_output_bytes() < required)
            || self.maximum_children == 0
            || self.tool_poll_interval.is_zero()
        {
            return Err(Fault::invalid_argument(
                "node agent configuration is invalid",
            ));
        }
        Ok(())
    }
}
