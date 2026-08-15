// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bio_formats::{SequenceRecord, parse_a3m, parse_fasta};
use mindclade_bounded_parse::{Limits, ParseMode};
use mindclade_faults::FaultResult;

pub fn fasta(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SequenceRecord>> {
    parse_fasta(bytes, limits, ParseMode::Strict)
}
pub fn a3m(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SequenceRecord>> {
    parse_a3m(bytes, limits, ParseMode::Strict)
}
