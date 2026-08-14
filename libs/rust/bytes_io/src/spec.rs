//! Checked byte sizes, ranges, alignment, and budgets.
#![forbid(unsafe_code)]

use mindclade_faults::{Code, Fault, FaultResult};
use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

/// A checked count of bytes.
#[derive(Clone, Copy, Default, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ByteSize(u64);

impl ByteSize {
    pub const ZERO: Self = Self(0);
    pub const KIB: Self = Self(1_024);
    pub const MIB: Self = Self(1_048_576);
    pub const GIB: Self = Self(1_073_741_824);
    #[must_use]
    pub const fn new(bytes: u64) -> Self {
        Self(bytes)
    }
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
    pub fn checked_add(self, other: Self) -> FaultResult<Self> {
        self.0
            .checked_add(other.0)
            .map(Self)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "byte-size addition overflow"))
    }
    pub fn checked_mul(self, factor: u64) -> FaultResult<Self> {
        self.0
            .checked_mul(factor)
            .map(Self)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "byte-size multiplication overflow"))
    }
}

impl fmt::Debug for ByteSize {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.debug_tuple("ByteSize").field(&self.0).finish()
    }
}

impl fmt::Display for ByteSize {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{}B", self.0)
    }
}

/// Half-open byte interval `[start, end)`.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct ByteRange {
    start: u64,
    length: u64,
}

impl ByteRange {
    pub fn new(start: u64, length: u64) -> FaultResult<Self> {
        start
            .checked_add(length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "byte range overflows"))?;
        Ok(Self { start, length })
    }
    #[must_use]
    pub const fn start(self) -> u64 {
        self.start
    }
    #[must_use]
    pub const fn length(self) -> u64 {
        self.length
    }
    #[must_use]
    pub const fn is_empty(self) -> bool {
        self.length == 0
    }
    #[must_use]
    pub fn end(self) -> u64 {
        self.start + self.length
    }
    #[must_use]
    pub fn contains(self, offset: u64) -> bool {
        offset >= self.start && offset < self.end()
    }
}

/// Power-of-two byte alignment.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Alignment(u64);

impl Alignment {
    pub fn new(value: u64) -> FaultResult<Self> {
        if value == 0 || !value.is_power_of_two() {
            return Err(Fault::invalid_argument("alignment must be a non-zero power of two"));
        }
        Ok(Self(value))
    }
    pub fn align_up(self, value: u64) -> FaultResult<u64> {
        let mask = self.0 - 1;
        value
            .checked_add(mask)
            .map(|candidate| candidate & !mask)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "aligned value overflows"))
    }
}

#[derive(Debug)]
struct BudgetState {
    limit: u64,
    used: AtomicU64,
}

/// Shared atomic byte budget.
#[derive(Clone, Debug)]
pub struct ByteBudget(Arc<BudgetState>);

impl ByteBudget {
    #[must_use]
    pub fn new(limit: ByteSize) -> Self {
        Self(Arc::new(BudgetState { limit: limit.get(), used: AtomicU64::new(0) }))
    }
    pub fn reserve(&self, amount: ByteSize) -> FaultResult<Reservation> {
        let amount = amount.get();
        let mut current = self.0.used.load(Ordering::Acquire);
        loop {
            let Some(next) = current.checked_add(amount) else {
                return Err(Fault::new(Code::ResourceExhausted, "byte budget overflow"));
            };
            if next > self.0.limit {
                return Err(Fault::new(Code::ResourceExhausted, "byte budget exceeded")
                    .with_context("limit_bytes", self.0.limit)
                    .with_context("requested_bytes", amount)
                    .with_context("used_bytes", current));
            }
            match self.0.used.compare_exchange_weak(
                current,
                next,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => return Ok(Reservation { state: Arc::clone(&self.0), amount }),
                Err(observed) => current = observed,
            }
        }
    }
    #[must_use]
    pub fn used(&self) -> ByteSize {
        ByteSize::new(self.0.used.load(Ordering::Acquire))
    }
    #[must_use]
    pub fn limit(&self) -> ByteSize {
        ByteSize::new(self.0.limit)
    }
}

/// RAII reservation released on drop.
#[derive(Debug)]
pub struct Reservation {
    state: Arc<BudgetState>,
    amount: u64,
}

impl Reservation {
    #[must_use]
    pub const fn amount(&self) -> ByteSize {
        ByteSize::new(self.amount)
    }
}

impl Drop for Reservation {
    fn drop(&mut self) {
        self.state.used.fetch_sub(self.amount, Ordering::AcqRel);
    }
}
