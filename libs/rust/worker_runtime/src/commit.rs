//! Fenced output-commit checks.

use mindclade_faults::FaultResult;
use mindclade_runtime_core::FencingToken;

/// Reject a stale worker before it can publish output or terminal status.
pub fn require_current(candidate: FencingToken, current: FencingToken) -> FaultResult<()> {
    candidate.require_current(current)
}
