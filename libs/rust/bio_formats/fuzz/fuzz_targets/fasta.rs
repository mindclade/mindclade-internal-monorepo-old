#![no_main]

use libfuzzer_sys::fuzz_target;
use mindclade_bounded_parse::{Limits, ParseMode};

fuzz_target!(|data: &[u8]| {
    let _ = mindclade_bio_formats::parse_fasta(data, Limits::default(), ParseMode::Strict);
});
