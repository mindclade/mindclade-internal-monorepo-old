# `mindclade_bio_formats` fuzz seed corpus

**Path:** `libs/rust/bio_formats/corpus/README.md`

Synthetic seed inputs for the `fuzz/` targets, one directory per target. Regenerate with:

```sh
tools/dev/nixw develop .#ci --command python3 libs/rust/bio_formats/corpus/seed.py
```

`seed.py` is the source of truth; the `.bin` files are its output and are committed so that a fuzz
run starts from a meaningful frontier rather than from random bytes.
`tools/qualification/rust/fuzz.py` copies them into a scratch corpus and points libFuzzer there.

**Do not pass this directory to libFuzzer directly.** `cargo fuzz run <target> corpus/<target>`
would make libFuzzer *write* its own generated inputs back into a tree whose entire policy is that
every byte is hand-written — copy the seeds somewhere writable first:

```sh
cp -R libs/rust/bio_formats/corpus/fasta /tmp/fasta-corpus
cargo fuzz run fasta /tmp/fasta-corpus -- -max_total_time=30
```

## What is in here, and what may never be

Every byte is hand-written and synthetic: a valid example per format plus the malformed shapes
that have already found defects — empty input, a header with no body, an unterminated mmCIF text
field, a MOL counts line that lies about its block, a lone SDF delimiter.

**No real biological data, partner data, or anything resembling patient data may be committed to
this repository.** That is why the corpus is generated from a script rather than downloaded from a
public database, and why the whole directory is under two kilobytes of payload.

## The 8-byte header

Each seed is prefixed with the limits header that `derive_limits` peels off in
`fuzz/src/lib.rs`. Without it the fuzzer would consume the first bytes of a fixture as parse
ceilings and never reach the format body. The seeds use generous ceilings with the input ceiling
tracking the body length, so a seed exercises the parser rather than the rejection path.
