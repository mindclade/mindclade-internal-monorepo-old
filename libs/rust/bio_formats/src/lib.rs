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
pub use common::{
    FastqRecord, Format, SequenceRecord, TextDocument
};
use mindclade_bounded_parse::{
    Limits, ParseMode
};
use mindclade_faults::FaultResult;

pub fn parse_fasta(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<SequenceRecord>> {
    fasta::parse(bytes, limits, mode)
}

pub fn parse_fastq(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<FastqRecord>> {
    fastq::parse(bytes, limits, mode)
}

pub fn parse_a3m(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<SequenceRecord>> {
    a3m::parse(bytes, limits, mode)
}

pub fn parse_stockholm(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SequenceRecord>> {
    stockholm::parse(bytes, limits)
}

pub fn parse_text_document(format: Format, bytes: &[u8], limits: Limits) -> FaultResult<TextDocument> {
    let records=match format {
        Format::Sdf=>sdf::parse(bytes, limits)?.into_iter().map(|r|r.bytes).collect(), Format::Mol=>vec![mol::parse(bytes,
        limits)?.bytes], Format::Pdb=>pdb::parse(bytes, limits)?.into_iter().map(|r|r.line).collect(), Format::Mmcif=>vec![mmcif::serialize(&mmcif::parse(bytes,
        limits)?)?], _=>vec![bytes.to_vec()]
    };
    Ok(TextDocument {
        format, records
    })
}
