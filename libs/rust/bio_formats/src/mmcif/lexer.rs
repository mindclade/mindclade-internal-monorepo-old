// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Conservative but complete-enough mmCIF token framing.
//!
//! This lexer handles comments, unquoted tokens, quoted tokens, and semicolon
//! text fields beginning in column one. It deliberately does not assign
//! scientific meaning to categories/loops; Python remains authoritative for
//! model-domain semantics.
use super::record::CifToken;
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn lex(bytes: &[u8], limits: Limits) -> FaultResult<Vec<CifToken>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut output = Vec::new();
    let mut text_field: Option<(usize, Vec<u8>)> = None;
    while let Some((location, line)) = cursor.next_line()? {
        if let Some((offset, value)) = text_field.as_mut() {
            // b';', not b'; ' — the latter is two codepoints and does not compile. A semicolon
            // in the first column is what closes an mmCIF multi-line text field; the space was
            // never part of the delimiter.
            if line.first() == Some(&b';') {
                push_token(&mut output, value, *offset, limits, &mut allocation)?;
                text_field = None;
            } else {
                allocation.charge_usize(line.len().checked_add(1).ok_or_else(|| {
                    Fault::new(Code::OutOfRange, "retained line allocation overflow")
                })?)?;
                value.extend_from_slice(line);
                value.push(b'\n');
            }
            continue;
        }
        // Same two-codepoint literal as above: the opening delimiter is a bare semicolon.
        // `&line[1..]` already assumes a one-byte delimiter, which is the other half of the
        // evidence that the space was a typo rather than intent.
        if line.first() == Some(&b';') {
            let initial = &line[1..];
            allocation.charge_usize(initial.len().checked_add(1).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "retained text allocation overflow")
            })?)?;
            let mut value = initial.to_vec();
            value.push(b'\n');
            text_field = Some((location.offset, value));
            continue;
        }
        lex_line(line, location.offset, limits, &mut allocation, &mut output)?;
    }
    if text_field.is_some() {
        return Err(Fault::invalid_argument(
            "mmCIF semicolon text field is unterminated",
        ));
    }
    Ok(output)
}

fn lex_line(
    line: &[u8],
    base_offset: usize,
    limits: Limits,
    allocation: &mut AllocationBudget,
    output: &mut Vec<CifToken>,
) -> FaultResult<()> {
    let mut index = 0_usize;
    while index < line.len() {
        while index < line.len() && line[index].is_ascii_whitespace() {
            index += 1;
        }
        if index >= line.len() || line[index] == b'#' {
            break;
        }
        let start = index;
        let token = if let quote @ (b'\'' | b'"') = line[index] {
            index += 1;
            let value_start = index;
            while index < line.len() && line[index] != quote {
                index += 1;
            }
            if index >= line.len() {
                return Err(
                    Fault::invalid_argument("mmCIF quoted token is unterminated").with_context(
                        "offset",
                        u64::try_from(base_offset.checked_add(start).ok_or_else(|| {
                            Fault::new(Code::OutOfRange, "mmCIF offset overflow")
                        })?)
                        .map_err(|_| Fault::new(Code::OutOfRange, "mmCIF offset exceeds u64"))?,
                    ),
                );
            }
            let value = &line[value_start..index];
            index += 1;
            if index < line.len() && !line[index].is_ascii_whitespace() && line[index] != b'#' {
                return Err(Fault::invalid_argument(
                    "mmCIF quoted token is not whitespace-delimited",
                ));
            }
            value
        } else {
            while index < line.len() && !line[index].is_ascii_whitespace() && line[index] != b'#' {
                index += 1;
            }
            &line[start..index]
        };
        let absolute_offset = base_offset
            .checked_add(start)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "mmCIF token offset overflow"))?;
        push_token(output, token, absolute_offset, limits, allocation)?;
        if index < line.len() && line[index] == b'#' {
            break;
        }
    }
    Ok(())
}

fn push_token(
    output: &mut Vec<CifToken>,
    bytes: &[u8],
    offset: usize,
    limits: Limits,
    allocation: &mut AllocationBudget,
) -> FaultResult<()> {
    if output.len() >= limits.maximum_tokens {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "mmCIF token limit exceeded",
        ));
    }
    let value = std::str::from_utf8(bytes)
        .map_err(|error| Fault::invalid_argument("mmCIF token is not UTF-8").with_source(error))?;
    if value.is_empty() {
        return Err(Fault::invalid_argument("mmCIF token is empty"));
    }
    allocation.charge_usize(value.len())?;
    let token = CifToken {
        value: value.to_owned(),
        offset,
    };
    token.validate()?;
    output.push(token);
    Ok(())
}
