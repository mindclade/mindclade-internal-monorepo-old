// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::common::{SequenceRecord, valid_sequence_byte};
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;

// The state-machine stays contiguous so header, record, and terminator bounds
// can be audited in protocol order.
#[allow(clippy::too_many_lines)]
pub fn parse(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SequenceRecord>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut saw_header = false;
    let mut saw_terminator = false;
    let mut total_tokens = 0_usize;
    let mut sequences: BTreeMap<String, Vec<u8>> = BTreeMap::new();
    while let Some((_location, line)) = cursor.next_line()? {
        if line == b"# STOCKHOLM 1.0" {
            if saw_header || !sequences.is_empty() {
                return Err(Fault::invalid_argument(
                    "Stockholm header is duplicated or misplaced",
                ));
            }
            saw_header = true;
            continue;
        }
        if line == b"//" {
            saw_terminator = true;
            break;
        }
        if line.is_empty() || line.starts_with(b"#") {
            continue;
        }
        if !saw_header {
            return Err(Fault::invalid_argument(
                "Stockholm data appears before header",
            ));
        }
        let text = std::str::from_utf8(line).map_err(|error| {
            Fault::invalid_argument("Stockholm line is not UTF-8").with_source(error)
        })?;
        let mut fields = text.split_whitespace();
        let id = fields
            .next()
            .ok_or_else(|| Fault::invalid_argument("Stockholm id missing"))?;
        let fragment = fields
            .next()
            .ok_or_else(|| Fault::invalid_argument("Stockholm sequence missing"))?;
        if fields.next().is_some() {
            return Err(Fault::invalid_argument(
                "Stockholm sequence line has unexpected fields",
            ));
        }
        if id.is_empty() || id.len() > 4096 {
            return Err(Fault::invalid_argument(
                "Stockholm id is empty or too large",
            ));
        }
        if fragment
            .as_bytes()
            .iter()
            .any(|value| !valid_sequence_byte(*value))
        {
            return Err(Fault::invalid_argument(
                "Stockholm sequence contains invalid byte",
            ));
        }
        if !sequences.contains_key(id) {
            if sequences.len() >= limits.maximum_records {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "Stockholm record limit exceeded",
                ));
            }
            allocation.charge_usize(id.len())?;
        }
        total_tokens = total_tokens
            .checked_add(fragment.len())
            .ok_or_else(|| Fault::new(Code::OutOfRange, "Stockholm token accounting overflow"))?;
        if total_tokens > limits.maximum_tokens {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "Stockholm token limit exceeded",
            ));
        }
        allocation.charge_usize(fragment.len())?;
        sequences
            .entry(id.to_owned())
            .or_default()
            .extend_from_slice(fragment.as_bytes());
    }
    if !saw_header {
        return Err(Fault::invalid_argument("Stockholm header missing"));
    }
    if !saw_terminator {
        return Err(Fault::invalid_argument("Stockholm terminator missing"));
    }
    if sequences.is_empty() {
        return Err(Fault::invalid_argument("Stockholm contains no sequences"));
    }
    sequences
        .into_iter()
        .map(|(id, sequence)| {
            let record = SequenceRecord {
                id,
                description: String::new(),
                sequence,
            };
            record.validate()?;
            Ok(record)
        })
        .collect()
}
