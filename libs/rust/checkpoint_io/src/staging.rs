use mindclade_bytes_io::{
    ByteBudget, ByteSize, Reservation
};
use mindclade_faults::FaultResult;

#[derive(Clone, Debug)]
pub struct StagingBudget {
    budget: ByteBudget
}

impl StagingBudget {
    #[must_use]pub fn new(limit: ByteSize) -> Self {
        Self {
            budget: ByteBudget::new(limit)
        }
    }
    pub fn reserve(&self, bytes: ByteSize) -> FaultResult<Reservation> {
        self.budget.reserve(bytes)
    }
    #[must_use]pub fn used(&self) -> ByteSize {
        self.budget.used()
    }
}
