//! Port contract implemented by the Rust online gateway deployment.

use crate::{InferenceRequest, ResponseChunk};
use mindclade_faults::FaultResult;

pub trait GatewayPort: Send + Sync {
    fn submit(&self, request: InferenceRequest, now_unix_millis: u64) -> FaultResult<String>;
    fn cancel(&self, request_id: &str, reason: &str) -> FaultResult<()>;
    fn poll(&self, request_id: &str) -> FaultResult<Option<ResponseChunk>>;
}
