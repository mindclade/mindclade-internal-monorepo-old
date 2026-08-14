//! Monotonic sequence validation for commands and status streams.

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Sequencer {
    last: u64,
}

impl Sequencer {
    #[must_use]
    pub const fn new() -> Self {
        Self { last: 0 }
    }
    pub fn observe(&mut self, sequence: u64) -> FaultResult<()> {
        if sequence == 0 || sequence <= self.last {
            return Err(Fault::new(
                Code::Conflict,
                "worker protocol sequence is stale or duplicated",
            ));
        }
        self.last = sequence;
        Ok(())
    }
    #[must_use]
    pub const fn last(self) -> u64 {
        self.last
    }
}
