#![no_main]

use libfuzzer_sys::fuzz_target;
use mindclade_bounded_parse::Limits;

fuzz_target!(|data: &[u8]| {
    let _ = mindclade_bio_formats::mmcif::parse(data, Limits::default());
});
