#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The `Format` dispatch itself.
//!
//! This target exists because of a defect no per-format target could have
//! found: `parse_text_document` ended in a `_ =>` arm that copied the input
//! whole for FASTA, A3M, FASTQ, and Stockholm, applying none of the `Limits` it
//! was handed. The per-format fuzzers all called the readers directly and so
//! walked straight past the entry point that skipped them.

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats::Format;
use mindclade_bio_formats_fuzz::derive_limits;

const FORMATS: [Format; 8] = [
    Format::Fasta,
    Format::Fastq,
    Format::A3m,
    Format::Stockholm,
    Format::Pdb,
    Format::Mmcif,
    Format::Sdf,
    Format::Mol,
];

fuzz_target!(|data: &[u8]| {
    let (limits, body) = derive_limits(data);
    // Take the format selector from the body so the fuzzer can steer it, and
    // fall back to a fixed choice when there is nothing left to select with.
    let (index, payload) = body.split_first().map_or((0, body), |(first, rest)| {
        (usize::from(*first) % FORMATS.len(), rest)
    });
    let _ = mindclade_bio_formats::parse_text_document(FORMATS[index], payload, limits);
});
