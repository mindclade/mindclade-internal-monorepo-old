// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bounded_parse::AllocationBudget;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Format {
    Fasta,
    Fastq,
    A3m,
    Stockholm,
    Pdb,
    Mmcif,
    Sdf,
    Mol,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SequenceRecord {
    pub id: String,
    pub description: String,
    pub sequence: Vec<u8>,
}
impl SequenceRecord {
    pub fn validate(&self) -> FaultResult<()> {
        if self.id.is_empty() || self.sequence.is_empty() {
            return Err(Fault::invalid_argument("sequence record is incomplete"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FastqRecord {
    pub id: String,
    pub description: String,
    pub sequence: Vec<u8>,
    pub quality: Vec<u8>,
}
impl FastqRecord {
    pub fn validate(&self) -> FaultResult<()> {
        if self.id.is_empty()
            || self.sequence.is_empty()
            || self.sequence.len() != self.quality.len()
        {
            return Err(Fault::invalid_argument("FASTQ record is incomplete"));
        }
        if self.quality.iter().any(|value| !(33..=126).contains(value)) {
            return Err(Fault::invalid_argument(
                "FASTQ quality byte is outside printable Phred range",
            ));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TextDocument {
    pub format: Format,
    pub records: Vec<Vec<u8>>,
}

pub(crate) fn parse_header(
    bytes: &[u8],
    kind: &str,
    allocation: &mut AllocationBudget,
) -> FaultResult<(String, String)> {
    let text = std::str::from_utf8(bytes).map_err(|error| {
        Fault::invalid_argument(format!("{kind} header is not UTF-8")).with_source(error)
    })?;
    let mut parts = text.splitn(2, char::is_whitespace);
    let id = parts.next().unwrap_or("").trim();
    if id.is_empty() || id.len() > 4096 {
        return Err(Fault::invalid_argument(format!(
            "{kind} record id is empty or too large"
        )));
    }
    let description = parts.next().unwrap_or("").trim();
    if description.len() > 1_048_576 {
        return Err(Fault::invalid_argument(format!(
            "{kind} description is too large"
        )));
    }
    allocation.charge_usize(
        id.len()
            .checked_add(description.len())
            .ok_or_else(|| Fault::invalid_argument("sequence header allocation overflow"))?,
    )?;
    Ok((id.to_owned(), description.to_owned()))
}

pub(crate) fn valid_sequence_byte(value: u8) -> bool {
    value.is_ascii_alphabetic() || matches!(value, b'-' | b'.' | b'*' | b'?')
}

pub(crate) fn push_sequence(
    target: &mut Vec<u8>,
    line: &[u8],
    total_tokens: &mut usize,
    maximum_tokens: usize,
    preserve_case: bool,
    strict: bool,
    allocation: &mut AllocationBudget,
) -> FaultResult<()> {
    for value in line
        .iter()
        .copied()
        .filter(|value| !value.is_ascii_whitespace())
    {
        if !valid_sequence_byte(value) {
            if strict {
                return Err(Fault::invalid_argument("sequence contains invalid byte"));
            }
            continue;
        }
        *total_tokens = total_tokens
            .checked_add(1)
            .ok_or_else(|| Fault::invalid_argument("sequence token accounting overflow"))?;
        if *total_tokens > maximum_tokens {
            return Err(Fault::invalid_argument("sequence token limit exceeded"));
        }
        allocation.charge(1)?;
        target.push(if preserve_case {
            value
        } else {
            value.to_ascii_uppercase()
        });
    }
    Ok(())
}
