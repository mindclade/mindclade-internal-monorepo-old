# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.errors import Code, default_message, is_canonical_code, normalize_code, parse_code

# The wire strings, spelled out rather than derived from Code. A test that builds its
# expectation from the same enum it is checking passes no matter what the enum says.
EXPECTED = {
    "unknown",
    "canceled",
    "invalid_argument",
    "deadline_exceeded",
    "not_found",
    "already_exists",
    "permission_denied",
    "unauthenticated",
    "resource_exhausted",
    "failed_precondition",
    "conflict",
    "aborted",
    "out_of_range",
    "not_implemented",
    "internal",
    "unavailable",
    "data_loss",
}


def test_code_set_is_exactly_the_declared_wire_contract() -> None:
    assert {code.value for code in Code} == EXPECTED


def test_code_is_its_wire_string() -> None:
    assert Code.NOT_FOUND == "not_found"
    assert f"{Code.DATA_LOSS}" == "data_loss"


@pytest.mark.parametrize("raw", ["not_found", "  not_found  ", "NOT_FOUND", "Not_Found"])
def test_parse_normalizes_space_and_ascii_case(raw: str) -> None:
    assert parse_code(raw) is Code.NOT_FOUND


def test_parse_rejects_an_unknown_code_rather_than_degrading() -> None:
    # Version skew has to be visible. Silently returning UNKNOWN here is how a code a
    # newer peer sent becomes an unexplained generic failure three services away.
    with pytest.raises(ValueError, match="invalid code"):
        parse_code("teapot")


def test_normalize_degrades_where_parse_raises() -> None:
    assert normalize_code("teapot") is Code.UNKNOWN
    assert normalize_code(" CONFLICT ") is Code.CONFLICT


def test_is_canonical_code_matches_the_member_set() -> None:
    assert is_canonical_code("aborted")
    assert not is_canonical_code("ABORTED")
    assert not is_canonical_code("teapot")


def test_every_code_has_a_client_safe_default_message() -> None:
    for code in Code:
        assert default_message(code)
    assert default_message(Code.UNKNOWN) == "operation failed"
    assert default_message("teapot") == "operation failed"
