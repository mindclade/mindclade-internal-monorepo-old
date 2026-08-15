// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic retry policy with injected sleeping.
#![forbid(unsafe_code)]

use crate::{Clock, Deadline};
use mindclade_faults::{Code, Fault, FaultResult, RetryHint};
use std::time::Duration;

/// Backoff policy. Jitter is deterministic for a given seed and attempt.
#[derive(Clone, Copy, Debug)]
pub struct Policy {
    pub max_attempts: u32,
    pub initial_delay: Duration,
    pub maximum_delay: Duration,
    pub multiplier_milli: u32,
    pub jitter_permyriad: u16,
}

impl Policy {
    pub fn validate(self) -> FaultResult<Self> {
        if self.max_attempts == 0 {
            return Err(Fault::invalid_argument(
                "retry max_attempts must be positive",
            ));
        }
        if self.initial_delay > self.maximum_delay {
            return Err(Fault::invalid_argument(
                "retry initial delay exceeds maximum delay",
            ));
        }
        if self.maximum_delay.as_nanos() > u128::from(u64::MAX) {
            return Err(Fault::new(
                Code::OutOfRange,
                "retry maximum delay exceeds nanosecond domain",
            ));
        }
        if self.multiplier_milli < 1_000 {
            return Err(Fault::invalid_argument(
                "retry multiplier must be at least 1.0",
            ));
        }
        if self.jitter_permyriad > 10_000 {
            return Err(Fault::invalid_argument("retry jitter exceeds 100 percent"));
        }
        Ok(self)
    }
    pub fn delay(self, attempt: u32, seed: u64) -> FaultResult<Duration> {
        let policy = self.validate()?;
        let maximum_nanos = policy.maximum_delay.as_nanos();
        let mut nanos = policy.initial_delay.as_nanos();
        for _ in 1..attempt {
            let scaled = nanos
                .checked_mul(u128::from(policy.multiplier_milli))
                .ok_or_else(|| {
                    Fault::new(Code::OutOfRange, "retry backoff multiplication overflow")
                })?;
            nanos = (scaled / 1_000).min(maximum_nanos);
        }
        let random_value = splitmix64(seed ^ u64::from(attempt)) % 20_001;
        let random = i128::from(
            i32::try_from(random_value)
                .map_err(|_| Fault::new(Code::OutOfRange, "retry random value exceeds i32"))?,
        ) - 10_000;
        let base = i128::try_from(nanos)
            .map_err(|_| Fault::new(Code::OutOfRange, "retry base delay exceeds i128"))?;
        let jitter = i128::from(policy.jitter_permyriad);
        let adjustment = base
            .checked_mul(jitter)
            .and_then(|value| value.checked_mul(random))
            .ok_or_else(|| Fault::new(Code::OutOfRange, "retry jitter calculation overflow"))?
            / 100_000_000;
        let adjusted = base
            .checked_add(adjustment)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "retry delay calculation overflow"))?
            .max(0);
        let capped = u128::try_from(adjusted)
            .map_err(|_| Fault::new(Code::OutOfRange, "retry delay became negative"))?
            .min(maximum_nanos);
        let nanos = u64::try_from(capped)
            .map_err(|_| Fault::new(Code::OutOfRange, "retry delay exceeds u64 nanoseconds"))?;
        Ok(Duration::from_nanos(nanos))
    }
}

impl Default for Policy {
    fn default() -> Self {
        Self {
            max_attempts: 5,
            initial_delay: Duration::from_millis(100),
            maximum_delay: Duration::from_secs(5),
            multiplier_milli: 2_000,
            jitter_permyriad: 2_000,
        }
    }
}

/// Sleeping abstraction used to keep tests deterministic.
pub trait Sleeper {
    fn sleep(&self, duration: Duration);
}

#[derive(Clone, Copy, Debug, Default)]
pub struct ThreadSleeper;
impl Sleeper for ThreadSleeper {
    fn sleep(&self, duration: Duration) {
        std::thread::sleep(duration);
    }
}

/// Executes a synchronous operation according to the supplied policy.
pub fn execute<T, F>(
    policy: Policy,
    clock: &dyn Clock,
    sleeper: &dyn Sleeper,
    deadline: Option<Deadline>,
    seed: u64,
    mut operation: F,
) -> FaultResult<T>
where
    F: FnMut(u32) -> FaultResult<T>,
{
    let policy = policy.validate()?;
    for attempt in 1..=policy.max_attempts {
        match operation(attempt) {
            Ok(value) => return Ok(value),
            Err(fault) => {
                if attempt == policy.max_attempts || !fault.retry_hint().is_retryable() {
                    return Err(fault.with_context("attempt", u64::from(attempt)));
                }
                let delay = match fault.retry_hint() {
                    RetryHint::After(value) => value,
                    RetryHint::Immediate => policy.delay(attempt, seed)?,
                    RetryHint::Never => Duration::ZERO,
                };
                if let Some(deadline) = deadline {
                    if deadline.is_expired(clock) || delay > deadline.remaining(clock) {
                        return Err(Fault::new(
                            Code::DeadlineExceeded,
                            "retry deadline would be exceeded",
                        )
                        .with_context("attempt", u64::from(attempt)));
                    }
                }
                sleeper.sleep(delay);
            }
        }
    }
    Err(Fault::internal("retry loop exhausted without a result"))
}

fn splitmix64(mut value: u64) -> u64 {
    value = value.wrapping_add(0x9e37_79b9_7f4a_7c15);
    value = (value ^ (value >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    value = (value ^ (value >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    value ^ (value >> 31)
}
