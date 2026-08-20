# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import json

import pytest

from libs.python.errors import Code, code_of
from libs.python.serialization import (
    canonical_field,
    canonical_json_bytes,
    canonical_lines,
)


def test_keys_are_sorted_regardless_of_insertion_order() -> None:
    assert canonical_json_bytes({"b": 1, "a": 2}) == canonical_json_bytes({"a": 2, "b": 1})
    assert canonical_json_bytes({"b": 1, "a": 2}) == b'{"a":2,"b":1}'


def test_no_insignificant_whitespace() -> None:
    assert canonical_json_bytes({"a": [1, 2], "b": {"c": 3}}) == b'{"a":[1,2],"b":{"c":3}}'


def test_non_ascii_is_emitted_as_utf8_not_escaped() -> None:
    # The divergence this module exists to close. ensure_ascii=True would emit
    # b'{"k":"caf\\u00e9"}', a different byte string and therefore a different
    # digest for the same document — and one Go and Rust would not produce.
    assert canonical_json_bytes({"k": "café"}) == '{"k":"café"}'.encode()


def test_utf8_encoding_matches_what_the_other_languages_emit() -> None:
    # The Greek alpha is the subject of this test, not a typo for "a". Confusable
    # characters are exactly what the encoding has to carry through unchanged, so
    # substituting an ASCII lookalike would delete the assertion. Hence the
    # suppressions on the two lines that carry one.
    document = {"organism": "Saccharomyces cerevisiae", "note": "α-helix ≥ 3 Å"}  # noqa: RUF001
    encoded = canonical_json_bytes(document)
    # Round-trips through a strict JSON parser, and carries the raw UTF-8 bytes
    # rather than \u escapes.
    assert json.loads(encoded.decode("utf-8")) == document
    assert "α".encode() in encoded  # noqa: RUF001
    assert b"\\u" not in encoded


def test_encoding_is_deterministic_across_calls() -> None:
    document = {"z": [3, 2, 1], "a": {"nested": {"deep": True}}, "m": None}
    assert canonical_json_bytes(document) == canonical_json_bytes(document)


@pytest.mark.parametrize("value", [float("nan"), float("inf"), float("-inf")])
def test_non_finite_floats_are_rejected(value: float) -> None:
    # Not JSON, and both Go and Rust refuse them. Failing here turns a remote
    # parse error into a local one.
    with pytest.raises(ValueError) as caught:
        canonical_json_bytes({"k": value})
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_circular_references_are_reported_as_a_fault() -> None:
    document: dict[str, object] = {}
    document["self"] = document
    with pytest.raises(ValueError) as caught:
        canonical_json_bytes(document)
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_empty_document_encodes() -> None:
    assert canonical_json_bytes({}) == b"{}"


def test_canonical_lines_joins_with_a_trailing_newline() -> None:
    assert canonical_lines(["a", "b"]) == b"a\nb\n"
    assert canonical_lines([]) == b"\n"


def test_canonical_lines_rejects_an_embedded_newline() -> None:
    with pytest.raises(ValueError, match="newline"):
        canonical_lines(["a\nb"])


def test_canonical_lines_is_utf8() -> None:
    assert canonical_lines(["café"]) == "café\n".encode()


def test_canonical_field_rejects_reserved_delimiters() -> None:
    # Without this the line encoding is not injective: a field containing a
    # vertical bar could impersonate a structural line.
    assert canonical_field("forward") == "forward"
    with pytest.raises(ValueError, match="delimiter"):
        canonical_field("a|b")
    with pytest.raises(ValueError, match="delimiter"):
        canonical_field("a\nb")


def test_canonical_field_rejects_empty() -> None:
    with pytest.raises(ValueError, match="empty"):
        canonical_field("")
