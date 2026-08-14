//! Validated GPU memory snapshots.
#![forbid(unsafe_code)]

use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MemorySnapshot {
    total: u64,
    reserved: u64,
}

impl MemorySnapshot {
    pub fn new(total: u64, reserved: u64) -> FaultResult<Self> {
        if total == 0 {
            return Err(Fault::invalid_argument("GPU memory total must be positive"));
        }
        if reserved > total {
            return Err(Fault::invalid_argument("GPU reserved memory exceeds total memory"));
        }
        Ok(Self { total, reserved })
    }
    #[must_use]
    pub const fn total(self) -> u64 {
        self.total
    }
    #[must_use]
    pub const fn reserved(self) -> u64 {
        self.reserved
    }
    #[must_use]
    pub fn available(self) -> u64 {
        // Constructor maintains reserved <= total.
        self.total - self.reserved
    }
    #[must_use]
    pub fn pressure_permyriad(self) -> u16 {
        let pressure = (u128::from(self.reserved) * 10_000) / u128::from(self.total);
        // reserved <= total means the result is always <= 10_000.
        u16::try_from(pressure).unwrap_or(10_000)
    }
}
