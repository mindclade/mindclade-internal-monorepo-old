// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use super::record::MolRecord;
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits) -> FaultResult<MolRecord> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    allocation.charge_usize(bytes.len())?;
    if bytes.is_empty() || bytes.contains(&0) {
        return Err(Fault::invalid_argument(
            "MOL input is empty or contains NUL",
        ));
    }
    let mut lines = Vec::new();
    while let Some((_location, line)) = cursor.next_line()? {
        if lines.len() >= limits.maximum_records {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "MOL line limit exceeded",
            ));
        }
        lines.push(line);
    }
    if lines.len() < 4 {
        return Err(Fault::invalid_argument("MOL file is missing counts line"));
    }
    let counts = std::str::from_utf8(lines[3]).map_err(|error| {
        Fault::invalid_argument("MOL counts line is not UTF-8").with_source(error)
    })?;
    if counts.contains("V2000") {
        validate_v2000(&lines, limits)?;
    } else if counts.contains("V3000") {
        validate_v3000(&lines)?;
    } else {
        return Err(Fault::invalid_argument(
            "MOL version marker is missing or unsupported",
        ));
    }
    let record = MolRecord {
        bytes: bytes.to_vec(),
    };
    record.validate()?;
    Ok(record)
}

fn validate_v2000(lines: &[&[u8]], limits: Limits) -> FaultResult<()> {
    let counts = std::str::from_utf8(lines[3]).map_err(|error| {
        Fault::invalid_argument("MOL V2000 counts line is invalid").with_source(error)
    })?;
    if counts.len() < 6 || !counts.is_ascii() {
        return Err(Fault::invalid_argument(
            "MOL V2000 counts line is too short or non-ASCII",
        ));
    }
    let atoms = counts[0..3]
        .trim()
        .parse::<usize>()
        .map_err(|error| Fault::invalid_argument("MOL atom count is invalid").with_source(error))?;
    let bonds = counts[3..6]
        .trim()
        .parse::<usize>()
        .map_err(|error| Fault::invalid_argument("MOL bond count is invalid").with_source(error))?;
    let structural = atoms
        .checked_add(bonds)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "MOL counts overflow"))?;
    if structural > limits.maximum_tokens {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "MOL atom/bond count exceeds token limit",
        ));
    }
    let minimum_lines = 4_usize
        .checked_add(structural)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "MOL line count overflow"))?;
    if lines.len() <= minimum_lines {
        return Err(Fault::invalid_argument(
            "MOL V2000 atom/bond block is truncated",
        ));
    }
    if !lines[minimum_lines..]
        .iter()
        .any(|line| line.starts_with(b"M  END"))
    {
        return Err(Fault::invalid_argument("MOL V2000 terminator is missing"));
    }
    Ok(())
}

fn validate_v3000(lines: &[&[u8]]) -> FaultResult<()> {
    let has_begin = lines
        .iter()
        .any(|line| line.starts_with(b"M  V30 BEGIN CTAB"));
    let has_end = lines
        .iter()
        .any(|line| line.starts_with(b"M  V30 END CTAB"));
    let has_m_end = lines.iter().any(|line| line.starts_with(b"M  END"));
    if !(has_begin && has_end && has_m_end) {
        return Err(Fault::invalid_argument(
            "MOL V3000 CTAB framing is incomplete",
        ));
    }
    Ok(())
}
