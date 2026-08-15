// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use super::record::CifDocument;
use mindclade_faults::{Fault, FaultResult};

pub fn serialize(document: &CifDocument) -> FaultResult<Vec<u8>> {
    document.validate()?;
    let mut output = Vec::new();
    for token in &document.tokens {
        if token.value.contains('\0') {
            return Err(Fault::invalid_argument("mmCIF token contains NUL"));
        }
        if token.value.contains('\n') {
            output.extend_from_slice(b";\n");
            output.extend_from_slice(token.value.as_bytes());
            if !token.value.ends_with('\n') {
                output.push(b'\n');
            }
            output.extend_from_slice(b";\n");
        } else if requires_quote(&token.value) {
            let quote = if !token.value.contains('\'') {
                b'\''
            } else if !token.value.contains('"') {
                b'"'
            } else {
                output.extend_from_slice(b";\n");
                output.extend_from_slice(token.value.as_bytes());
                output.extend_from_slice(b"\n;\n");
                continue;
            };
            output.push(quote);
            output.extend_from_slice(token.value.as_bytes());
            output.push(quote);
            output.push(b'\n');
        } else {
            output.extend_from_slice(token.value.as_bytes());
            output.push(b'\n');
        }
    }
    Ok(output)
}

fn requires_quote(value: &str) -> bool {
    value.bytes().any(|byte| byte.is_ascii_whitespace())
        || value.starts_with('#')
        || value.starts_with(';')
}
