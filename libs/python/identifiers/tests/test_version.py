# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.errors import Code, code_of
from libs.python.identifiers import Digest, ResourceVersion, is_canonical_resource_version

DIGEST_TEXT = "sha256:" + "a" * 64
FIXTURE = f"rv1:42:{DIGEST_TEXT}"


def test_parses_the_cross_language_fixture_version() -> None:
    version = ResourceVersion.parse(FIXTURE)
    assert version.generation == 42
    assert version.digest.text == DIGEST_TEXT
    assert version.text == FIXTURE


def test_round_trips_through_text() -> None:
    version = ResourceVersion(7, Digest.of(b"state"))
    assert ResourceVersion.parse(version.text) == version


@pytest.mark.parametrize(
    "value",
    [
        "",
        "rv1",
        f"rv1:{DIGEST_TEXT}",
        f"rv2:1:{DIGEST_TEXT}",
        f"rv1:0:{DIGEST_TEXT}",
        f"rv1:-1:{DIGEST_TEXT}",
        f"rv1:1.5:{DIGEST_TEXT}",
        "rv1:1:sha256:" + "A" * 64,
        "rv1:1:" + "a" * 64,
        f"rv1:1:{DIGEST_TEXT}:extra",
    ],
)
def test_non_canonical_versions_are_rejected(value: str) -> None:
    assert not is_canonical_resource_version(value)
    with pytest.raises(ValueError):
        ResourceVersion.parse(value)


def test_leading_zero_generations_are_rejected() -> None:
    # "rv1:007:..." and "rv1:7:..." would otherwise be two spellings of one
    # version, and this token is compared as a string in conditional requests.
    assert not is_canonical_resource_version(f"rv1:007:{DIGEST_TEXT}")
    with pytest.raises(ValueError):
        ResourceVersion.parse(f"rv1:007:{DIGEST_TEXT}")


def test_generation_zero_is_not_constructible() -> None:
    with pytest.raises(ValueError, match="start at 1"):
        ResourceVersion(0, Digest.of(b"x"))


def test_parse_failure_is_an_invalid_argument_fault() -> None:
    with pytest.raises(ValueError) as caught:
        ResourceVersion.parse("nope")
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_next_advances_the_generation_and_takes_the_new_digest() -> None:
    first = ResourceVersion(1, Digest.of(b"a"))
    second = first.next(Digest.of(b"b"))
    assert second.generation == 2
    assert second.digest == Digest.of(b"b")


def test_next_leaves_the_original_untouched() -> None:
    first = ResourceVersion(1, Digest.of(b"a"))
    first.next(Digest.of(b"b"))
    assert first.generation == 1


def test_a_generation_that_advanced_without_a_content_change_is_visible() -> None:
    # The reason the digest travels with the counter at all.
    first = ResourceVersion(1, Digest.of(b"same"))
    second = first.next(Digest.of(b"same"))
    assert first != second
    assert first.digest == second.digest


def test_version_is_hashable_and_immutable() -> None:
    version = ResourceVersion.parse(FIXTURE)
    assert len({version, ResourceVersion.parse(FIXTURE)}) == 1
    with pytest.raises(AttributeError):
        version.generation = 2  # type: ignore[misc]
