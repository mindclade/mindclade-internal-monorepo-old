# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import hashlib
import io

import pytest

from libs.python.errors import Code, code_of
from libs.python.identifiers import (
    DIGEST_HEX_LENGTH,
    DIGEST_TEXT_LENGTH,
    Digest,
    is_canonical_digest,
)

LOWER = "sha256:" + "a" * 64
UPPER = "sha256:" + "A" * 64


def test_canonical_text_length_is_the_documented_constant() -> None:
    assert DIGEST_TEXT_LENGTH == 71
    assert DIGEST_HEX_LENGTH == 64
    assert len(Digest.of(b"").text) == DIGEST_TEXT_LENGTH


def test_digest_matches_hashlib() -> None:
    assert Digest.of(b"hello").hex == hashlib.sha256(b"hello").hexdigest()


def test_of_text_uses_utf8_not_the_platform_encoding() -> None:
    # The digest of a string has to be the same on every host and agree with Go.
    assert Digest.of_text("café") == Digest.of("café".encode())


def test_parse_round_trips() -> None:
    digest = Digest.of(b"payload")
    assert Digest.parse(digest.text) == digest


def test_uppercase_hex_is_rejected_by_both_predicate_and_parser() -> None:
    # This is the divergence that mattered: the old `startswith` + `len == 71`
    # checks accepted this string while the regex check rejected it.
    assert is_canonical_digest(LOWER)
    assert not is_canonical_digest(UPPER)
    with pytest.raises(ValueError):
        Digest.parse(UPPER)


@pytest.mark.parametrize(
    "value",
    [
        "",
        "sha256:",
        "a" * 64,
        "sha256:" + "a" * 63,
        "sha256:" + "a" * 65,
        "sha512:" + "a" * 64,
        "sha256:" + "g" * 64,
        " " + LOWER,
    ],
)
def test_non_canonical_forms_are_rejected(value: str) -> None:
    assert not is_canonical_digest(value)
    with pytest.raises(ValueError):
        Digest.parse(value)


def test_parse_failure_is_an_invalid_argument_fault() -> None:
    with pytest.raises(ValueError) as caught:
        Digest.parse("nope")
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_non_text_predicate_and_parser_inputs_fail_cleanly() -> None:
    assert not is_canonical_digest(None)
    with pytest.raises(ValueError) as caught:
        Digest.parse(None)  # type: ignore[arg-type]
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_construction_rejects_a_wrong_length_payload() -> None:
    with pytest.raises(ValueError, match="32 bytes"):
        Digest(b"\x00" * 31)

    with pytest.raises(ValueError, match="bytes"):
        Digest("x" * 32)  # type: ignore[arg-type]


def test_of_reader_streams_and_reports_the_byte_count() -> None:
    payload = b"x" * (3 << 20)
    digest, consumed = Digest.of_reader(io.BytesIO(payload))
    assert consumed == len(payload)
    assert digest == Digest.of(payload)


def test_of_reader_on_empty_input() -> None:
    digest, consumed = Digest.of_reader(io.BytesIO(b""))
    assert consumed == 0
    assert digest == Digest.of(b"")


def test_of_reader_rejects_a_text_stream() -> None:
    with pytest.raises(ValueError, match="return bytes"):
        Digest.of_reader(io.StringIO("payload"))  # type: ignore[arg-type]


def test_equals_is_value_based() -> None:
    assert Digest.of(b"a").equals(Digest.of(b"a"))
    assert not Digest.of(b"a").equals(Digest.of(b"b"))


def test_digest_is_hashable_and_immutable() -> None:
    assert len({Digest.of(b"a"), Digest.of(b"a")}) == 1
    with pytest.raises(AttributeError):
        Digest.of(b"a").raw = b"\x00" * 32  # type: ignore[misc]


def test_str_is_the_canonical_text() -> None:
    digest = Digest.of(b"a")
    assert str(digest) == digest.text
