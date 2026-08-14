//! Bounded lexical representation used by the conservative mmCIF parser.
use mindclade_faults::{
    Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CifToken {
    pub value: String,
    pub offset: usize
}

impl CifToken {
    pub fn validate(&self) -> FaultResult<()> {
        if self.value.is_empty() || self.value.len() > 1_048_576 {
            return Err(Fault::invalid_argument("mmCIF token is empty or too large"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CifDocument {
    pub tokens: Vec<CifToken>
}

impl CifDocument {
    pub fn validate(&self) -> FaultResult<()> {
        if self.tokens.is_empty() {
            return Err(Fault::invalid_argument("mmCIF document has no tokens"));
        }
        for token in &self.tokens {
            token.validate()?;
        }
        Ok(())
    }
}
