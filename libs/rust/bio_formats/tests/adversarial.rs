// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Hostile-input regressions for every format this crate frames.
//!
//! Every fixture here is synthetic and kilobyte-sized on purpose. Proving a
//! missing bound by actually allocating gigabytes would wedge CI, so each test
//! shrinks `Limits` to the point where a few hundred bytes of adversarial input
//! is already over the ceiling. Nothing in this file may resemble a real
//! biological dataset.

use mindclade_bio_formats::{
    Format, a3m, fasta, fastq, mmcif, mol, parse_text_document, pdb, sdf, stockholm,
};
use mindclade_bounded_parse::{Limits, ParseMode};
use mindclade_bytes_io::ByteSize;

const ALL_FORMATS: [Format; 8] = [
    Format::Fasta,
    Format::Fastq,
    Format::A3m,
    Format::Stockholm,
    Format::Pdb,
    Format::Mmcif,
    Format::Sdf,
    Format::Mol,
];

const MOL_FIXTURE: &[u8] = b"example\n\
  Mindclade\n\
comment\n\
  1  0  0  0  0  0            999 V2000\n\
    0.0000    0.0000    0.0000 C   0  0  0  0  0  0  0  0  0  0  0  0\n\
M  END\n";

/// Deliberately tiny ceilings: an adversarial fixture only has to cross *these*
/// numbers to prove the bound is enforced.
fn tight() -> Limits {
    Limits {
        maximum_input_bytes: ByteSize::new(4096),
        maximum_line_bytes: 64,
        maximum_records: 4,
        maximum_tokens: 256,
        maximum_metadata_entries: 8,
        maximum_nesting: 8,
        maximum_allocation_bytes: ByteSize::new(4096),
    }
}

fn valid_fixture(format: Format) -> Vec<u8> {
    match format {
        Format::Fasta => b">p1 desc\nACDEF\n".to_vec(),
        Format::A3m => b">q\nACde-F\n".to_vec(),
        Format::Fastq => b"@r1\nACGT\n+\nIIII\n".to_vec(),
        Format::Stockholm => b"# STOCKHOLM 1.0\np1 AC-D\n//\n".to_vec(),
        Format::Pdb => b"ATOM      1  CA  ALA A   1\nEND\n".to_vec(),
        Format::Mmcif => b"data_demo\n_entry.id demo\n".to_vec(),
        Format::Mol => MOL_FIXTURE.to_vec(),
        Format::Sdf => [MOL_FIXTURE, b"$$$$\n"].concat(),
    }
}

// ---------------------------------------------------------------------------
// Input ceiling
// ---------------------------------------------------------------------------

#[test]
fn document_entry_point_enforces_the_input_ceiling_for_every_format() {
    // The wildcard arm this replaced accepted `limits` and then ignored it for
    // the four sequence formats, copying the whole untrusted blob instead.
    let limits = tight();
    let oversized = vec![b'A'; 8192];
    assert!(oversized.len() > usize::try_from(limits.maximum_input_bytes.get()).expect("ceiling"));
    for format in ALL_FORMATS {
        assert!(
            parse_text_document(format, &oversized, limits).is_err(),
            "{format:?} accepted an input above maximum_input_bytes"
        );
    }
}

#[test]
fn document_entry_point_rejects_malformed_input_for_every_format() {
    // Framing-only formats and semantic formats alike must fail closed rather
    // than hand an unvalidated blob back to the caller.
    let limits = Limits::default();
    let malformed: [(Format, &[u8]); 8] = [
        (Format::Fasta, b"ACGT\n"),
        (Format::A3m, b"ACGT\n"),
        (Format::Fastq, b"@r\nACG\n+\nI\n"),
        (Format::Stockholm, b"p1 ACGT\n"),
        (Format::Pdb, b"\0\0\0\n"),
        (Format::Mmcif, b"data_demo\n;unterminated\n"),
        (Format::Sdf, b"$$$$\n"),
        (Format::Mol, b"not-a-mol\n"),
    ];
    for (format, bytes) in malformed {
        assert!(
            parse_text_document(format, bytes, limits).is_err(),
            "{format:?} accepted malformed input"
        );
    }
}

// ---------------------------------------------------------------------------
// Line ceiling: one enormous line with no newline
// ---------------------------------------------------------------------------

#[test]
fn a_single_oversized_line_is_rejected_by_every_parser() {
    let limits = tight();
    let long = vec![b'A'; 1024];
    let mut sequence_input = b">h\n".to_vec();
    sequence_input.extend_from_slice(&long);

    assert!(fasta::parse(&sequence_input, limits, ParseMode::Strict).is_err());
    assert!(a3m::parse(&sequence_input, limits, ParseMode::Strict).is_err());
    assert!(
        fastq::parse(
            &[b"@h\n".as_slice(), &long].concat(),
            limits,
            ParseMode::Strict
        )
        .is_err()
    );
    assert!(
        stockholm::parse(
            &[b"# STOCKHOLM 1.0\nid ".as_slice(), &long].concat(),
            limits
        )
        .is_err()
    );
    assert!(pdb::parse(&[b"ATOM  ".as_slice(), &long].concat(), limits).is_err());
    assert!(mmcif::parse(&[b"data_x\n".as_slice(), &long].concat(), limits).is_err());
    assert!(sdf::parse(&long, limits).is_err());
    assert!(mol::parse(&long, limits).is_err());
}

/// Builds `data_x\n_a\n;\n<lines>;\n` — one mmCIF semicolon text field of
/// `lines` continuation lines, each 32 bytes plus its newline.
fn mmcif_text_field(lines: usize) -> Vec<u8> {
    let mut input = b"data_x\n_a\n;\n".to_vec();
    for _ in 0..lines {
        input.extend_from_slice(b"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n");
    }
    input.extend_from_slice(b";\n");
    input
}

#[test]
fn mmcif_multiline_text_field_longer_than_one_line_still_parses() {
    // A semicolon text field exists precisely to carry content longer than one
    // line, so the accumulator must not be charged the per-line ceiling: a
    // caller who tightens `maximum_line_bytes` to a format-appropriate value
    // must still be able to read a well-formed `_struct.title`.
    let limits = Limits {
        maximum_line_bytes: 64,
        ..Limits::default()
    };
    let document = mmcif::parse(&mmcif_text_field(20), limits).expect("well-formed text field");
    assert!(document.tokens.iter().any(|token| token.value.len() > 64));
}

#[test]
fn mmcif_text_field_is_bounded_by_the_allocation_ceiling() {
    // 20 lines accumulate ~660 bytes into one retained token, well past a
    // 512-byte retained-allocation ceiling.
    let limits = Limits {
        maximum_input_bytes: ByteSize::new(2048),
        maximum_line_bytes: 64,
        maximum_allocation_bytes: ByteSize::new(512),
        ..Limits::default()
    };
    assert!(mmcif::parse(&mmcif_text_field(20), limits).is_err());
}

// ---------------------------------------------------------------------------
// Record-count ceiling
// ---------------------------------------------------------------------------

#[test]
fn record_counts_are_capped_for_every_accumulating_parser() {
    let limits = tight();
    let maximum = limits.maximum_records;

    let sequence_input = ">h\nAC\n".repeat(maximum * 4);
    assert!(fasta::parse(sequence_input.as_bytes(), limits, ParseMode::Strict).is_err());
    assert!(a3m::parse(sequence_input.as_bytes(), limits, ParseMode::Strict).is_err());

    let read_input = "@h\nAC\n+\nII\n".repeat(maximum * 4);
    assert!(fastq::parse(read_input.as_bytes(), limits, ParseMode::Strict).is_err());

    let mut stockholm_input = b"# STOCKHOLM 1.0\n".to_vec();
    for index in 0..maximum * 4 {
        stockholm_input.extend_from_slice(format!("id{index} AC\n").as_bytes());
    }
    stockholm_input.extend_from_slice(b"//\n");
    assert!(stockholm::parse(&stockholm_input, limits).is_err());

    let pdb_input = "ATOM  x\n".repeat(maximum * 4);
    assert!(pdb::parse(pdb_input.as_bytes(), limits).is_err());

    let sdf_input = "A\n$$$$\n".repeat(maximum * 4);
    assert!(sdf::parse(sdf_input.as_bytes(), limits).is_err());
}

#[test]
fn mmcif_token_count_is_capped() {
    // 40 lines of 8 tokens is 320 tokens against a ceiling of 256, in 647 bytes
    // — comfortably under `maximum_input_bytes`, so it is the token cap and not
    // the input cap that has to reject this.
    let limits = tight();
    let mut input = b"data_x\n".to_vec();
    for _ in 0..40 {
        input.extend_from_slice(b"t t t t t t t t\n");
    }
    assert!(input.len() < usize::try_from(limits.maximum_input_bytes.get()).expect("ceiling"));
    assert!(mmcif::parse(&input, limits).is_err());
}

// ---------------------------------------------------------------------------
// Declared counts are never trusted for allocation
// ---------------------------------------------------------------------------

#[test]
fn mol_declared_counts_do_not_drive_allocation() {
    // The counts line claims 999 atoms and 999 bonds in a six-line file. A
    // parser that trusted the declaration would reserve for 1998 records; this
    // one must reject the truncated block instead.
    let counts_lie = b"example\n\
  Mindclade\n\
comment\n\
999999  0  0  0  0            999 V2000\n\
    0.0000    0.0000    0.0000 C   0  0  0  0  0  0  0  0  0  0  0  0\n\
M  END\n";
    assert!(mol::parse(counts_lie, Limits::default()).is_err());
    assert!(mol::parse(counts_lie, tight()).is_err());
}

// ---------------------------------------------------------------------------
// Truncation and hostile bytes never panic
// ---------------------------------------------------------------------------

#[test]
fn every_prefix_of_every_valid_fixture_parses_or_faults_without_panicking() {
    // Deterministic stand-in for the fuzz corpus this crate still owes
    // qualification: truncate each fixture at every byte boundary.
    for format in ALL_FORMATS {
        let fixture = valid_fixture(format);
        for end in 0..=fixture.len() {
            let _ = parse_text_document(format, &fixture[..end], Limits::default());
            let _ = parse_text_document(format, &fixture[..end], tight());
        }
    }
}

#[test]
fn hostile_byte_patterns_never_panic() {
    let hostile: [&[u8]; 14] = [
        b"",
        b"\0",
        b"\xff\xfe\xfd",
        b">",
        b">\n",
        b"@",
        b"@\n\n\n",
        b"+",
        b";",
        b";\n",
        b"$$$$",
        b"//",
        b"# STOCKHOLM 1.0\n",
        b"data_\n",
    ];
    for bytes in hostile {
        for format in ALL_FORMATS {
            let _ = parse_text_document(format, bytes, Limits::default());
            let _ = parse_text_document(format, bytes, tight());
        }
        let _ = fasta::parse(bytes, Limits::default(), ParseMode::Recovery);
        let _ = a3m::parse(bytes, Limits::default(), ParseMode::Recovery);
        let _ = fastq::parse(bytes, Limits::default(), ParseMode::Recovery);
    }
}
