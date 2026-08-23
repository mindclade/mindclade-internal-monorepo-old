// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Shared fuzz harness support: turn fuzzer bytes into parse limits.
//!
//! Every target used to hardcode `Limits::default()`, which meant the fuzzer
//! only ever explored the 64 MiB input / 1 MiB line path and could never reach
//! the ceiling logic those limits exist to drive. Carving the ceilings out of
//! the fuzzer's own input makes the boundaries part of the search space.
//!
//! The header is decoded by hand rather than via `arbitrary` on purpose: this
//! crate is a separate cargo workspace, and adding a dependency to it churns a
//! lockfile that CI resolves with `--locked`.

use mindclade_bounded_parse::Limits;
use mindclade_bytes_io::ByteSize;

/// Bytes consumed from the front of the input to build `Limits`.
const HEADER_BYTES: usize = 8;

/// Splits fuzzer input into parse limits and the payload to parse.
///
/// Inputs shorter than the header parse under `Limits::default()`, so a corpus
/// seeded with plain format fixtures stays meaningful.
#[must_use]
pub fn derive_limits(data: &[u8]) -> (Limits, &[u8]) {
    if data.len() < HEADER_BYTES {
        return (Limits::default(), data);
    }
    let (header, body) = data.split_at(HEADER_BYTES);

    // Every ceiling is at least 1: `Limits::validate` rejects zero, and a
    // target that only ever produced invalid limits would fuzz nothing but the
    // validator.
    let maximum_line_bytes = 1 + usize::from(header[0]) * 64;
    let maximum_records = 1 + usize::from(header[1]);
    let maximum_tokens = 1 + usize::from(header[2]) * 16;
    let maximum_metadata_entries = 1 + usize::from(header[3]);
    let maximum_nesting = 1 + usize::from(header[4]);
    let derived_payload = 1024 + u64::from(header[5]) * 1024;

    // The input ceiling gets both regimes. Most runs admit the whole body, so
    // the parser is actually entered and its interior ceilings are what get
    // explored; one header bit instead pins the ceiling tight so that
    // `Source::new`'s own rejection path keeps getting hit.
    let body_length = u64::try_from(body.len()).unwrap_or(u64::MAX);
    let maximum_input_bytes = if header[7] & 1 == 1 {
        1 + u64::from(header[6])
    } else {
        body_length.max(1)
    };

    // `Limits::validate` requires input <= payload * 4; keep the payload budget
    // large enough that a valid header never fails for that reason alone.
    let maximum_payload_bytes = derived_payload.max(maximum_input_bytes.div_ceil(4));

    let limits = Limits {
        maximum_input_bytes: ByteSize::new(maximum_input_bytes),
        maximum_line_bytes,
        maximum_records,
        maximum_tokens,
        maximum_metadata_entries,
        maximum_nesting,
        maximum_payload_bytes: ByteSize::new(maximum_payload_bytes),
    };
    (limits, body)
}

#[cfg(test)]
mod tests {
    use super::{HEADER_BYTES, Limits, derive_limits};

    /// Header bytes worth probing: both extremes, the low non-zero value, and a
    /// midpoint. Ceilings are derived by multiplication, so 0 and 255 are where
    /// a derivation would produce a zero or an overflow.
    const PROBES: [u8; 4] = [0, 1, 127, 255];

    /// Drives one seed through the same call its target makes.
    fn run_seed(directory: &str, body: &[u8], limits: Limits) {
        use mindclade_bio_formats::{Format, mmcif, mol, parse_text_document, pdb, sdf};
        use mindclade_bounded_parse::ParseMode;

        match directory {
            "fasta" => {
                let _ = mindclade_bio_formats::parse_fasta(body, limits, ParseMode::Recovery);
            }
            "a3m" => {
                let _ = mindclade_bio_formats::parse_a3m(body, limits, ParseMode::Recovery);
            }
            "fastq" => {
                let _ = mindclade_bio_formats::parse_fastq(body, limits, ParseMode::Recovery);
            }
            "stockholm" => {
                let _ = mindclade_bio_formats::parse_stockholm(body, limits);
            }
            "pdb" => {
                let _ = pdb::parse(body, limits);
            }
            "mmcif" => {
                let _ = mmcif::parse(body, limits);
            }
            "sdf" => {
                let _ = sdf::parse(body, limits);
            }
            "mol" => {
                let _ = mol::parse(body, limits);
            }
            "text_document" => {
                // Mirrors the dispatch target: one selector byte, then the payload.
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
                let (index, payload) = body.split_first().map_or((0, body), |(first, rest)| {
                    (usize::from(*first) % FORMATS.len(), rest)
                });
                let _ = parse_text_document(FORMATS[index], payload, limits);
            }
            other => panic!("corpus directory {other} has no matching fuzz target"),
        }
    }

    #[test]
    fn every_derived_limit_is_one_the_validator_accepts() {
        // If this ever regressed, the targets would still run and still report
        // no findings — they would simply be fuzzing `Limits::validate` instead
        // of the parsers. That silent uselessness is what this test exists for.
        for a in PROBES {
            for b in PROBES {
                for c in PROBES {
                    for d in PROBES {
                        let header = [a, b, c, d, a, b, c, d];
                        for body_length in [0_usize, 1, 7, 64, 4096] {
                            let mut input = header.to_vec();
                            input.extend(std::iter::repeat_n(b'A', body_length));
                            let (limits, body) = derive_limits(&input);
                            assert_eq!(body.len(), body_length, "body was not returned intact");
                            limits.validate().unwrap_or_else(|error| {
                                panic!("header {header:?} produced invalid limits: {error}")
                            });
                        }
                    }
                }
            }
        }
    }

    #[test]
    fn input_shorter_than_the_header_falls_through_to_defaults() {
        for length in 0..HEADER_BYTES {
            let input = vec![b'>'; length];
            let (limits, body) = derive_limits(&input);
            assert_eq!(body, input.as_slice(), "short input must not be consumed");
            limits.validate().expect("defaults are valid");
        }
    }

    #[test]
    fn every_committed_corpus_seed_reaches_a_parser_without_panicking() {
        // `cargo-fuzz` is not in the `.#ci` shell, so this is the only evidence
        // available here that the seeds actually compose with the harness: a
        // seed whose header convention drifted would be silently truncated to
        // nothing by `derive_limits` and fuzz an empty slice forever.
        let corpus = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .parent()
            .expect("fuzz crate has a parent")
            .join("corpus");
        let mut seeds = 0_usize;
        for format_dir in std::fs::read_dir(&corpus).expect("corpus directory") {
            let format_dir = format_dir.expect("corpus entry").path();
            if !format_dir.is_dir() {
                continue;
            }
            for seed in std::fs::read_dir(&format_dir).expect("format directory") {
                let path = seed.expect("seed entry").path();
                if path.extension().is_none_or(|ext| ext != "bin") {
                    continue;
                }
                let data = std::fs::read(&path).expect("seed bytes");
                let (limits, body) = derive_limits(&data);
                limits
                    .validate()
                    .unwrap_or_else(|error| panic!("{}: invalid limits: {error}", path.display()));
                assert!(
                    u64::try_from(body.len()).expect("body length")
                        <= limits.maximum_input_bytes.get(),
                    "{}: seed header does not admit its own body",
                    path.display()
                );
                // Route each seed to the reader its directory names, so an
                // mmCIF or MOL seed actually reaches its own parser rather than
                // being fed to FASTA and rejected on line one.
                let directory = format_dir
                    .file_name()
                    .and_then(|name| name.to_str())
                    .expect("corpus directory name");
                run_seed(directory, body, limits);
                seeds += 1;
            }
        }
        assert!(
            seeds >= 30,
            "expected the committed corpus, found {seeds} seeds"
        );
    }

    #[test]
    fn both_input_ceiling_regimes_are_reachable() {
        // One header bit selects between admitting the whole body and pinning
        // the ceiling tight; losing either regime would quietly halve coverage.
        let body = [b'A'; 512];

        let mut admitting = vec![0, 0, 0, 0, 0, 0, 0, 0];
        admitting.extend_from_slice(&body);
        let (loose, _) = derive_limits(&admitting);
        assert_eq!(loose.maximum_input_bytes.get(), 512);

        let mut pinning = vec![0, 0, 0, 0, 0, 0, 9, 1];
        pinning.extend_from_slice(&body);
        let (tight, _) = derive_limits(&pinning);
        assert_eq!(tight.maximum_input_bytes.get(), 10);
    }
}
