// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded, deterministic readers and canonical serializers for common biological exchange formats.
#![forbid(unsafe_code)]
pub mod a3m;
pub mod common;
pub mod fasta;
pub mod fastq;
pub mod mmcif;
pub mod mol;
pub mod pdb;
pub mod sdf;
pub mod stockholm;
pub use common::{FastqRecord, Format, SequenceRecord, TextDocument};
use mindclade_bounded_parse::{Limits, ParseMode};
use mindclade_faults::FaultResult;

pub fn parse_fasta(
    bytes: &[u8],
    limits: Limits,
    mode: ParseMode,
) -> FaultResult<Vec<SequenceRecord>> {
    fasta::parse(bytes, limits, mode)
}

pub fn parse_fastq(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<FastqRecord>> {
    fastq::parse(bytes, limits, mode)
}

pub fn parse_a3m(
    bytes: &[u8],
    limits: Limits,
    mode: ParseMode,
) -> FaultResult<Vec<SequenceRecord>> {
    a3m::parse(bytes, limits, mode)
}

pub fn parse_stockholm(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SequenceRecord>> {
    stockholm::parse(bytes, limits)
}

/// Frames untrusted bytes of `format` into a bounded transport document.
///
/// The match is exhaustive on purpose. It previously ended in
/// `_ => vec![bytes.to_vec()]`, which took `limits` and then ignored every one
/// of them for FASTA, A3M, FASTQ, and Stockholm: a hostile blob of any size was
/// copied whole, with no input ceiling, no allocation accounting, and no
/// validation at all — the four formats most likely to arrive from a public
/// database or a user upload were the four that never reached `bounded_parse`.
/// A wildcard arm is fail-open by construction here, so a new `Format` must now
/// fail to compile rather than silently inherit the unbounded path.
pub fn parse_text_document(
    format: Format,
    bytes: &[u8],
    limits: Limits,
) -> FaultResult<TextDocument> {
    let records = match format {
        // Sequence formats are validated by their semantic reader and then
        // retained losslessly; the parse result is discarded because this entry
        // point's contract is bounded framing, not record extraction. Each copy
        // below is bounded by `maximum_input_bytes`, which the reader's
        // `Source::new` has already enforced on the very same slice — a second
        // `AllocationBudget` here would be an independent ledger that rejects
        // files the reader itself accepts, not a tighter bound.
        Format::Fasta => {
            fasta::parse(bytes, limits, ParseMode::Strict)?;
            vec![bytes.to_vec()]
        }
        Format::A3m => {
            a3m::parse(bytes, limits, ParseMode::Strict)?;
            vec![bytes.to_vec()]
        }
        Format::Fastq => {
            fastq::parse(bytes, limits, ParseMode::Strict)?;
            vec![bytes.to_vec()]
        }
        Format::Stockholm => {
            stockholm::parse(bytes, limits)?;
            vec![bytes.to_vec()]
        }
        Format::Sdf => sdf::parse(bytes, limits)?
            .into_iter()
            .map(|r| r.bytes)
            .collect(),
        Format::Mol => vec![mol::parse(bytes, limits)?.bytes],
        Format::Pdb => pdb::parse(bytes, limits)?
            .into_iter()
            .map(|r| r.line)
            .collect(),
        Format::Mmcif => vec![mmcif::serialize(&mmcif::parse(bytes, limits)?)?],
    };
    Ok(TextDocument { format, records })
}
