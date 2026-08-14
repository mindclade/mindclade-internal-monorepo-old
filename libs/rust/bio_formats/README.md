# `mindclade_bio_formats`

Bounded biological format handling built on `bounded_parse`. FASTA, A3M, and
Stockholm have semantic readers. PDB/mmCIF/SDF/MOL currently expose bounded
record/document framing only; Python remains the scientific semantic authority
until per-format differential, corpus, and fuzz qualification is complete.

Production promotion for each parser requires malformed corpora, truncation,
round-trip/canonical serialization where applicable, and fuzz coverage.
