//! Bounded parser diagnostic with byte/line/column location.
use crate::Location;
use mindclade_faults::{
    Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Diagnostic {
    pub location: Location,
    pub code: &'static str,
    pub message: String
}

impl Diagnostic {
    pub fn validate(&self) -> FaultResult<()> {
        if self.code.is_empty() || self.code.len() > 128 || self.message.len() > 4096 {
            return Err(Fault::invalid_argument("parser diagnostic exceeds bounds"));
        }
        Ok(())
    }
}
