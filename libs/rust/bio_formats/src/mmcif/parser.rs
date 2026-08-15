// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use super::{lexer, record::CifDocument};
use mindclade_bounded_parse::Limits;
use mindclade_faults::{Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits) -> FaultResult<CifDocument> {
    let tokens = lexer::lex(bytes, limits)?;
    if tokens.is_empty() {
        return Err(Fault::invalid_argument("mmCIF contains no tokens"));
    }
    if !tokens
        .iter()
        .any(|token| token.value.starts_with("data_") && token.value.len() > 5)
    {
        return Err(Fault::invalid_argument("mmCIF named data block is missing"));
    }
    let document = CifDocument { tokens };
    document.validate()?;
    Ok(document)
}
