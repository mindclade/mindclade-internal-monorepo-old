# `mindclade_bio_formats`

Bounded biological format handling built on `bounded_parse`. FASTA, A3M, and
Stockholm have semantic readers. PDB/mmCIF/SDF/MOL currently expose bounded
record/document framing only; Python remains the scientific semantic authority
until per-format differential, corpus, and fuzz qualification is complete.

Every format reaches `bounded_parse` — there is no arm of this crate that reads
untrusted bytes without a `Cursor` and an `AllocationBudget`, and
`parse_text_document` is exhaustive over `Format` so a new one cannot be added
without choosing its bounded reader.

Production promotion for each parser requires malformed corpora, truncation,
round-trip/canonical serialization where applicable, and fuzz coverage.
`tests/adversarial.rs` is the deterministic floor under that: oversized lines,
record-count and token-count ceilings, lying declared counts, prefix truncation
of every fixture, and hostile byte patterns. Fixtures there are synthetic and
kilobyte-sized by policy — no real biological data belongs in this repository.
