# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Three-language conformance for content digest text.

What this replaces: one assertion that a fixture string matched a locally
re-declared regular expression. It consulted no implementation and would have
passed with every digest parser in the tree disagreeing.

ADR-0004 makes every dataset, checkpoint, model bundle and evidence record
addressable by digest, which is only worth something if one digest has exactly
one spelling. The rules are therefore: the literal prefix ``sha256:``, then
exactly 64 lowercase hexadecimal characters, and nothing else — no uppercase, no
alternate algorithm, no length slack.

This pins the canonical vectors against the real Python implementation and reads
the constants out of the Go and Rust sources, the same source-inspection
technique ``test_wire_compatibility.py`` uses: this module runs in the Python
lane, which has no Go and no Rust toolchain, and a test that skips wherever a
toolchain is missing is not a test.

One live divergence, tracked below rather than asserted away. Rust's parser
(``libs/rust/content_digest/src/digest.rs``) decodes ``b'A'..=b'F'`` as well as
``b'a'..=b'f'``, so ``sha256:AAAA…`` is admitted by the Rust data plane and
rejected by ``libs/go/identifiers.ParseDigest`` and
``libs/python/identifiers.Digest.parse`` — one content address with two
spellings across a language boundary. The fix is deleting one arm of that
``nibble`` match, in a crate this change does not own, so
``test_rust_rejects_uppercase_hexadecimal`` records it as a strict xfail:
red CI is not shipped, and the day ``content_digest`` is tightened the xpass
fails the suite and forces this marker out. It is a tracker, not an exemption.
"""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path

import pytest

from libs.python.errors import InvalidArgument
from libs.python.identifiers import (
    DIGEST_ALGORITHM,
    DIGEST_HEX_LENGTH,
    DIGEST_PREFIX,
    DIGEST_TEXT_LENGTH,
    Digest,
    is_canonical_digest,
)

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = Path(__file__).parent / "fixtures" / "primitives_v1.json"

GO_DIGEST = ROOT / "libs/go/identifiers/digest.go"
RUST_DIGEST = ROOT / "libs/rust/content_digest/src/digest.rs"

# The contract, stated once.
ALGORITHM = "sha256"
PREFIX = "sha256:"
HEX_LENGTH = 64
TEXT_LENGTH = len(PREFIX) + HEX_LENGTH

_A = "a" * HEX_LENGTH
_EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()

ACCEPTED = (
    f"{PREFIX}{_A}",
    f"{PREFIX}{'0' * HEX_LENGTH}",
    f"{PREFIX}{'f' * HEX_LENGTH}",
    f"{PREFIX}{_EMPTY_SHA256}",
)

REJECTED = (
    "",
    _A,  # bare hexadecimal, no algorithm
    f"{PREFIX}{_A.upper()}",  # uppercase is a second spelling of one address
    f"{PREFIX}{_A[:-1]}",  # one character short
    f"{PREFIX}{_A}a",  # one character long
    f"{PREFIX}{'g' * HEX_LENGTH}",  # not hexadecimal
    f"sha512:{_A}",  # wrong algorithm, right shape
    f"SHA256:{_A}",  # uppercase algorithm
    f"sha256{_A}",  # missing colon
    f"{PREFIX} {_A[:-1]}",  # leading space inside the payload
)


def _quoted_constant(path: Path, name: str) -> str:
    match = re.search(rf"\b{name}\b[^=\n]*=\s*\"([^\"]*)\"", path.read_text(encoding="utf-8"))
    assert match, f"{path.relative_to(ROOT)} no longer declares {name}"
    return match.group(1)


def _collapsed(path: Path) -> str:
    """The file with runs of whitespace collapsed to one space.

    gofmt aligns a `const` block to its longest name, so matching Go source
    verbatim pins the number of spaces around `=` — adding an unrelated, longer
    constant to the same block would re-align it and fail a conformance test with
    nothing about the contract changed.
    """
    return re.sub(r"\s+", " ", path.read_text(encoding="utf-8"))


def _extract(path: Path, pattern: str, what: str) -> str:
    """Pull one captured value out of a source file, or say what went missing."""
    match = re.search(pattern, _collapsed(path))
    assert match, (
        f"{path.relative_to(ROOT)} no longer spells {what} where this test reads it. "
        "Re-point the pattern at wherever it moved rather than deleting the check."
    )
    return match.group(1)


def test_python_declares_the_contract() -> None:
    assert DIGEST_ALGORITHM == ALGORITHM
    assert DIGEST_PREFIX == PREFIX
    assert DIGEST_HEX_LENGTH == HEX_LENGTH
    assert DIGEST_TEXT_LENGTH == TEXT_LENGTH


def test_go_declares_the_contract() -> None:
    assert _quoted_constant(GO_DIGEST, "DigestAlgorithm") == ALGORITHM
    # Go composes the lengths from crypto/sha256 rather than writing numbers, so
    # the composition is what there is to pin.
    assert _extract(GO_DIGEST, r"DigestBinarySize = ([\w.]+)", "its digest size") == "sha256.Size"
    assert _extract(GO_DIGEST, r"DigestHexLength = sha256.Size \* (\d+)", "its hex width") == "2"
    assert (
        _extract(
            GO_DIGEST,
            r"DigestTextLength = len\(DigestAlgorithm\) \+ (\d+) \+ DigestHexLength",
            "its text length",
        )
        == "1"
    )
    # The rule Go states in code and Python states in `is_canonical_digest`: one
    # byte-for-byte representation, so the uppercase spelling is not a digest.
    assert "hexValue != strings.ToLower(hexValue)" in _collapsed(GO_DIGEST)


def test_rust_agrees_on_prefix_and_length() -> None:
    assert _extract(RUST_DIGEST, r'strip_prefix\("([^"]*)"\)', "the digest prefix") == PREFIX
    assert _extract(RUST_DIGEST, r"encoded.len\(\) != (\d+)", "the digest hex width") == str(
        HEX_LENGTH
    )


@pytest.mark.xfail(
    strict=True,
    reason=(
        "libs/rust/content_digest decodes b'A'..=b'F', so Rust admits an uppercase "
        "digest that Go and Python reject. Fixing it means deleting one arm of that "
        "crate's `nibble` match. When that lands this xpasses, which fails the suite "
        "under strict=True and forces this marker out."
    ),
)
def test_rust_rejects_uppercase_hexadecimal() -> None:
    uppercase = re.search(r"b'A'\.\.=b'F'", _collapsed(RUST_DIGEST))
    assert uppercase is None, "Rust still decodes uppercase hexadecimal digests"


@pytest.mark.parametrize("value", ACCEPTED)
def test_python_accepts_canonical_digest(value: str) -> None:
    assert is_canonical_digest(value)
    digest = Digest.parse(value)
    assert digest.text == value
    assert digest.hex == value[len(PREFIX) :]
    assert len(digest.raw) == HEX_LENGTH // 2


@pytest.mark.parametrize("value", REJECTED)
def test_python_rejects_noncanonical_digest(value: str) -> None:
    assert not is_canonical_digest(value)
    with pytest.raises(InvalidArgument):
        Digest.parse(value)


def test_python_digest_of_bytes_matches_the_reference_hash() -> None:
    # The digest of the empty input is the one vector every SHA-256 in the tree
    # can be checked against without sharing code with the thing under test.
    assert Digest.of(b"").text == f"{PREFIX}{_EMPTY_SHA256}"
    assert Digest.of_text("mindclade").text == (
        f"{PREFIX}{hashlib.sha256(b'mindclade').hexdigest()}"
    )


def test_fixture_round_trips_through_the_python_implementation() -> None:
    data = json.loads(FIXTURE.read_text(encoding="utf-8"))
    digest = Digest.parse(data["digest"])
    assert digest.text == data["digest"]
    assert len(data["digest"]) == TEXT_LENGTH
    # The artifact reference and the resource version in the same fixture address
    # the same bytes; a fixture that spelled the digest two ways would defeat the
    # whole point of a content address.
    assert data["artifact_ref"]["digest"] == data["digest"]
    assert data["resource_version"].endswith(data["digest"])
