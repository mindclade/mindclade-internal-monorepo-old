#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Write the synthetic fuzz seed corpus for `mindclade_bio_formats`.

The seeds mirror the fixtures in `tests/adversarial.rs`: a valid example per
format plus the malformed shapes that already found defects (empty input, a
header with no body, an unterminated mmCIF text field, a MOL counts line that
lies about its block, a lone SDF delimiter).

Every byte here is synthetic and hand-written. No real biological data, partner
data, or anything resembling patient data may be committed to this repository,
so the corpus is generated rather than downloaded.

Each seed is prefixed with the 8-byte limits header that `derive_limits` peels
off, so a seed exercises the parser rather than being consumed as ceilings.
Header bytes chosen for generous ceilings with the input ceiling tracking the
body (`header[7] & 1 == 0`).
"""

from __future__ import annotations

import pathlib

ROOT = pathlib.Path(__file__).resolve().parent

# line=16321, records=256, tokens=4081, metadata=256, nesting=64,
# payload=1024+255*1024, input=len(body), regime bit clear.
HEADER = bytes([255, 255, 255, 255, 63, 255, 0, 0])

MOL = (
    b"example\n"
    b"  Mindclade\n"
    b"comment\n"
    b"  1  0  0  0  0  0            999 V2000\n"
    b"    0.0000    0.0000    0.0000 C   0  0  0  0  0  0  0  0  0  0  0  0\n"
    b"M  END\n"
)

SEEDS: dict[str, dict[str, bytes]] = {
    "fasta": {
        "valid": b">p1 desc\nACDEF\n",
        "multi_record": b">a\nACGT\n>b\nTGCA\n",
        "header_only": b">p1\n",
        "body_before_header": b"ACGT\n",
        "empty": b"",
    },
    "a3m": {
        "valid": b">q\nACde-F\n",
        "insertions": b">q\nAC...de-F\n",
        "empty": b"",
    },
    "fastq": {
        "valid": b"@r1\nACGT\n+\nIIII\n",
        "length_mismatch": b"@r1\nACG\n+\nII\n",
        "truncated": b"@r1\nACGT\n+\n",
        "empty": b"",
    },
    "stockholm": {
        "valid": b"# STOCKHOLM 1.0\np1 AC-D\n//\n",
        "interleaved": b"# STOCKHOLM 1.0\np1 AC-D\np1 EF--\n//\n",
        "no_terminator": b"# STOCKHOLM 1.0\np1 AC\n",
        "no_header": b"p1 ACGT\n//\n",
    },
    "pdb": {
        "valid": b"ATOM      1  CA  ALA A   1\nEND\n",
        "nul_byte": b"ATOM  \x00\n",
        "empty": b"",
    },
    "mmcif": {
        "valid": b"data_demo\n_entry.id demo\n",
        "quoted": b"data_demo\n_struct.title 'demo structure'\n",
        "text_field": b"data_demo\n_a\n;\nmultiline\nvalue\n;\n",
        "unterminated_text_field": b"data_demo\n;unterminated\n",
        "no_data_block": b"_entry.id demo\n",
    },
    "sdf": {
        "valid": MOL + b"$$$$\n",
        "two_records": MOL + b"$$$$\n" + MOL + b"$$$$\n",
        "delimiter_only": b"$$$$\n",
    },
    "mol": {
        "v2000": MOL,
        "counts_lie": (
            b"example\n"
            b"  Mindclade\n"
            b"comment\n"
            b"999999  0  0  0  0            999 V2000\n"
            b"    0.0000    0.0000    0.0000 C   0  0  0  0  0  0  0  0  0  0  0  0\n"
            b"M  END\n"
        ),
        "no_version": b"not-a-mol\n",
    },
}

# The dispatch target takes one selector byte off the front of the body.
TEXT_DOCUMENT_SELECTOR = {
    "fasta": 0,
    "fastq": 1,
    "a3m": 2,
    "stockholm": 3,
    "pdb": 4,
    "mmcif": 5,
    "sdf": 6,
    "mol": 7,
}


def main() -> int:
    written = 0
    for format_name, cases in SEEDS.items():
        directory = ROOT / format_name
        directory.mkdir(exist_ok=True)
        for case_name, body in cases.items():
            (directory / f"{case_name}.bin").write_bytes(HEADER + body)
            written += 1

    dispatch = ROOT / "text_document"
    dispatch.mkdir(exist_ok=True)
    for format_name, selector in TEXT_DOCUMENT_SELECTOR.items():
        body = next(iter(SEEDS[format_name].values()))
        (dispatch / f"{format_name}.bin").write_bytes(HEADER + bytes([selector]) + body)
        written += 1

    print(f"wrote {written} synthetic corpus seeds")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
