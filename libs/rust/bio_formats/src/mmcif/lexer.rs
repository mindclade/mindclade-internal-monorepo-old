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
use super::record::{CifToken, MAXIMUM_TOKEN_BYTES};
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};

/// Largest number of bytes any single token may retain.
///
/// Deliberately *not* `maximum_line_bytes`: a semicolon text field exists to
/// carry content longer than one line, so charging it the per-line ceiling would
/// reject well-formed mmCIF (`_struct.title` and friends) from any caller who
/// tightened that ceiling. The two real bounds are the format's own token
/// invariant and the parse's retained-allocation budget — a token above either
/// could never survive to be returned anyway, so applying them here only moves
/// the rejection earlier.
fn token_ceiling(limits: Limits) -> usize {
    usize::try_from(limits.maximum_allocation_bytes.get())
        .unwrap_or(MAXIMUM_TOKEN_BYTES)
        .min(MAXIMUM_TOKEN_BYTES)
}

fn charge_text_field(
    value: &mut Vec<u8>,
    line: &[u8],
    limits: Limits,
    allocation: &mut AllocationBudget,
) -> FaultResult<()> {
    let growth = line
        .len()
        .checked_add(1)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "retained line allocation overflow"))?;
    let next = value
        .len()
        .checked_add(growth)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "mmCIF text field accounting overflow"))?;
    if next > token_ceiling(limits) {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "mmCIF text field exceeds parse limit",
        ));
    }
    allocation.charge_usize(growth)?;
    value.extend_from_slice(line);
    value.push(b'\n');
    Ok(())
}

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
                charge_text_field(value, line, limits, &mut allocation)?;
            }
            continue;
        }
        // Same two-codepoint literal as above: the opening delimiter is a bare semicolon.
        // `&line[1..]` already assumes a one-byte delimiter, which is the other half of the
        // evidence that the space was a typo rather than intent.
        if line.first() == Some(&b';') {
            let mut value = Vec::new();
            charge_text_field(&mut value, &line[1..], limits, &mut allocation)?;
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
    // Checked before the copy below, not after: `CifToken::validate` used to be
    // the only size gate, which meant an oversized token was fully materialised
    // into a `String` before being rejected.
    if bytes.len() > token_ceiling(limits) {
        return Err(
            Fault::new(Code::ResourceExhausted, "mmCIF token exceeds parse limit").with_context(
                "offset",
                u64::try_from(offset)
                    .map_err(|_| Fault::new(Code::OutOfRange, "mmCIF offset exceeds u64"))?,
            ),
        );
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
