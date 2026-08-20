// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded runtime-gateway configuration.

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GatewayConfig {
    pub maximum_active_requests: u32,
    pub maximum_active_requests_per_grant: u32,
    pub maximum_stream_chunks: usize,
    pub maximum_stream_chunk_bytes: usize,
    pub maximum_stream_output_bytes: u64,
    pub maximum_request_key_bytes: usize,
    pub require_fresh_route_snapshot: bool,
    pub pod_memory_limit_bytes: u64,
    pub request_buffer_budget_bytes: u64,
    pub response_buffer_budget_bytes: u64,
    pub execution_enabled: bool,
}

impl GatewayConfig {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_active_requests == 0
            || self.maximum_active_requests_per_grant == 0
            || self.maximum_active_requests_per_grant > self.maximum_active_requests
            || self.maximum_stream_chunks == 0
            || self.maximum_stream_chunks > 65_536
            || self.maximum_stream_chunk_bytes == 0
            || self.maximum_stream_chunk_bytes > 16 * 1024 * 1024
            || self.maximum_stream_output_bytes == 0
            || self.maximum_request_key_bytes == 0
            || self.maximum_request_key_bytes > 16 * 1024
            || self.pod_memory_limit_bytes == 0
            || self.request_buffer_budget_bytes == 0
            || self.response_buffer_budget_bytes == 0
            || self
                .request_buffer_budget_bytes
                .checked_add(self.response_buffer_budget_bytes)
                .is_none_or(|managed| managed > self.pod_memory_limit_bytes / 2)
        {
            return Err(Fault::invalid_argument(
                "runtime gateway limits are invalid",
            ));
        }
        Ok(())
    }

    pub fn with_memory_limit(mut self, pod_memory_limit_bytes: u64) -> FaultResult<Self> {
        if pod_memory_limit_bytes < 64 * 1024 * 1024 {
            return Err(Fault::invalid_argument(
                "runtime gateway pod memory limit is too small",
            ));
        }
        self.pod_memory_limit_bytes = pod_memory_limit_bytes;
        self.request_buffer_budget_bytes = pod_memory_limit_bytes / 5;
        self.response_buffer_budget_bytes = pod_memory_limit_bytes
            .checked_div(10)
            .and_then(|tenth| tenth.checked_mul(3))
            .ok_or_else(|| Fault::new(Code::OutOfRange, "memory budget overflow"))?;
        self.validate()?;
        Ok(self)
    }
}

impl Default for GatewayConfig {
    fn default() -> Self {
        Self {
            maximum_active_requests: 4_096,
            maximum_active_requests_per_grant: 128,
            maximum_stream_chunks: 256,
            maximum_stream_chunk_bytes: 1024 * 1024,
            maximum_stream_output_bytes: 64 * 1024 * 1024,
            maximum_request_key_bytes: 4 * 1024,
            require_fresh_route_snapshot: true,
            pod_memory_limit_bytes: 1024 * 1024 * 1024,
            request_buffer_budget_bytes: 1024 * 1024 * 1024 / 5,
            response_buffer_budget_bytes: (1024 * 1024 * 1024 * 3) / 10,
            execution_enabled: false,
        }
    }
}
