// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded lexical representation used by the conservative mmCIF parser.
use mindclade_faults::{Fault, FaultResult};

/// Largest single token this crate will materialise, as a type invariant.
///
/// The lexer checks this ceiling *before* copying, so a hostile semicolon text
/// field is rejected at the ceiling instead of after the whole field has been
/// buffered and cloned into a `String`.
pub(crate) const MAXIMUM_TOKEN_BYTES: usize = 1_048_576;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CifToken {
    pub value: String,
    pub offset: usize,
}

impl CifToken {
    pub fn validate(&self) -> FaultResult<()> {
        if self.value.is_empty() || self.value.len() > MAXIMUM_TOKEN_BYTES {
            return Err(Fault::invalid_argument("mmCIF token is empty or too large"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CifDocument {
    pub tokens: Vec<CifToken>,
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
