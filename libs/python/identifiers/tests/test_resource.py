# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.errors import Code, MindcladeError, code_of
from libs.python.identifiers import (
    COUNTER_SPACE,
    GUARANTEED_PER_MILLISECOND,
    UUID_VERSION,
    IdGenerator,
    ResourceId,
    is_canonical_kind,
    is_canonical_resource_id,
    new_resource_id,
    new_resource_id_at,
    parse_kind,
)

FIXTURE_ID = "run_019c00000000700080000000000000aa"


def test_parses_the_cross_language_fixture_identifier() -> None:
    identifier = ResourceId.parse(FIXTURE_ID)
    assert identifier.kind == "run"
    assert identifier.text == FIXTURE_ID


@pytest.mark.parametrize("kind", ["run", "ab", "model", "a" + "b" * 23])
def test_valid_kinds(kind: str) -> None:
    assert is_canonical_kind(kind)
    assert parse_kind(kind) == kind


@pytest.mark.parametrize("kind", ["", "a", "A", "Run", "1run", "run_x", "run-x", "a" + "b" * 24])
def test_invalid_kinds(kind: str) -> None:
    assert not is_canonical_kind(kind)
    with pytest.raises(ValueError):
        parse_kind(kind)


@pytest.mark.parametrize(
    "value",
    [
        "",
        "run",
        "run_",
        "019c00000000700080000000000000aa",
        "Run_019c00000000700080000000000000aa",
        "run_019C00000000700080000000000000AA",
        "run_019c00000000700080000000000000a",
        "run_019c00000000700080000000000000aaa",
    ],
)
def test_non_canonical_identifiers_are_rejected(value: str) -> None:
    assert not is_canonical_resource_id(value)
    with pytest.raises(ValueError):
        ResourceId.parse(value)


def test_payload_must_be_a_version_7_rfc_variant_uuid() -> None:
    # Textually well formed, but version nibble 4 rather than 7.
    with pytest.raises(ValueError, match="version 7"):
        ResourceId.parse("run_019c00000000400080000000000000aa")
    # Version 7, but a non-RFC variant in byte 8.
    with pytest.raises(ValueError, match="RFC variant"):
        ResourceId.parse("run_019c00000000700000000000000000aa")


def test_parse_failure_is_an_invalid_argument_fault() -> None:
    with pytest.raises(ValueError) as caught:
        ResourceId.parse("nope")
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_minted_identifiers_satisfy_the_cross_language_bit_assertions() -> None:
    # The same two checks tests/integration/cross_language/test_identifiers.py makes.
    raw = new_resource_id("run").raw
    assert raw[6] >> 4 == UUID_VERSION
    assert raw[8] & 0xC0 == 0x80


def test_minted_identifiers_round_trip() -> None:
    identifier = new_resource_id("model")
    assert ResourceId.parse(identifier.text) == identifier


def test_timestamp_is_recoverable() -> None:
    assert new_resource_id_at("job", 1_700_000_000_000).unix_millis == 1_700_000_000_000


def test_identifiers_sort_by_creation_time_within_a_kind() -> None:
    early = new_resource_id_at("job", 1_600_000_000_000).text
    late = new_resource_id_at("job", 1_700_000_000_000).text
    assert early < late


def test_a_same_millisecond_burst_is_monotonic_and_unique() -> None:
    # The seed is drawn from the lower half of the 12-bit counter, so this many
    # is always available at one stamp.
    minted = [
        new_resource_id_at("job", 1_700_000_000_000).text for _ in range(GUARANTEED_PER_MILLISECOND)
    ]
    assert minted == sorted(minted)
    assert len(set(minted)) == len(minted)


def test_exhausting_one_millisecond_raises_rather_than_moving_the_timestamp() -> None:
    # raw_at promises the exact stamp, so it cannot borrow the next millisecond
    # the way wall-clock minting does. Running out has to be visible.
    generator = IdGenerator()
    with pytest.raises(MindcladeError) as caught:
        for _ in range(COUNTER_SPACE + 1):
            generator.raw_at(1_700_000_000_000)
    assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED


def test_wall_clock_minting_borrows_the_next_millisecond_instead_of_raising() -> None:
    # It promises "now, in order" rather than an exact stamp, so advancing keeps
    # uniqueness without breaking a promise.
    generator = IdGenerator(clock=lambda: 1_700_000_000_000)
    minted = [ResourceId("job", generator.raw_now()).text for _ in range(COUNTER_SPACE + 1)]
    assert minted == sorted(minted)
    assert len(set(minted)) == len(minted)


def test_an_explicit_timestamp_is_honored_not_clamped() -> None:
    # The two mint paths guarantee different things on purpose. An explicit stamp
    # is the caller stamping a moment, so clamping it to "whatever the last call
    # used" would embed a time they never asked for and could not detect.
    new_resource_id("job")
    assert new_resource_id_at("job", 1_600_000_000_000).unix_millis == 1_600_000_000_000


def test_wall_clock_minting_does_not_go_backwards_across_a_clock_step() -> None:
    # NTP correction or a suspended host. Injecting the clock is the only way to
    # exercise this; the guard is what keeps these IDs range-scannable.
    readings = iter([1_700_000_100_000, 1_600_000_000_000, 1_600_000_000_001])
    generator = IdGenerator(clock=lambda: next(readings))
    minted = [ResourceId("job", generator.raw_now()).text for _ in range(3)]
    assert minted == sorted(minted)
    assert len(set(minted)) == 3


def test_an_isolated_generator_does_not_share_state_with_the_default() -> None:
    generator = IdGenerator(clock=lambda: 1_700_000_000_000)
    first = ResourceId("job", generator.raw_now())
    new_resource_id_at("job", 1_900_000_000_000)
    second = ResourceId("job", generator.raw_now())
    assert second.unix_millis == first.unix_millis


def test_timestamps_outside_48_bits_are_rejected() -> None:
    with pytest.raises(ValueError, match="48 bits"):
        new_resource_id_at("job", -1)
    with pytest.raises(ValueError, match="48 bits"):
        new_resource_id_at("job", 1 << 48)


def test_identifier_is_hashable_and_immutable() -> None:
    identifier = ResourceId.parse(FIXTURE_ID)
    assert len({identifier, ResourceId.parse(FIXTURE_ID)}) == 1
    with pytest.raises(AttributeError):
        identifier.kind = "job"  # type: ignore[misc]
