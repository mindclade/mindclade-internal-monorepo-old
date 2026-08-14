//! Stable default retry classification for transport-neutral faults.
use crate::{Code, RetryHint};
#[must_use]
pub const fn default_retry_hint(code: Code) -> RetryHint { if code.is_transient() { RetryHint::Immediate } else { RetryHint::Never } }
#[must_use]
pub const fn retryable(code: Code) -> bool { !matches!(default_retry_hint(code), RetryHint::Never) }
