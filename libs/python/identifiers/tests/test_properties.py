# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from hypothesis import given
from hypothesis import strategies as st

from libs.python.identifiers import Digest, IdGenerator, ResourceId, ResourceVersion

KINDS = st.from_regex(r"[a-z][a-z0-9]{1,23}", fullmatch=True)


@given(kind=KINDS, unix_millis=st.integers(min_value=0, max_value=(1 << 48) - 1))
def test_resource_id_parse_render_round_trip(kind: str, unix_millis: int) -> None:
    identifier = ResourceId(kind, IdGenerator().raw_at(unix_millis))

    assert ResourceId.parse(identifier.text) == identifier
    assert str(identifier) == identifier.text
    assert identifier.unix_millis == unix_millis


@given(data=st.binary(max_size=4_096), generation=st.integers(min_value=1, max_value=(1 << 64) - 1))
def test_digest_and_resource_version_round_trip(data: bytes, generation: int) -> None:
    digest = Digest.of(data)
    version = ResourceVersion(generation, digest)

    assert Digest.parse(digest.text) == digest
    assert ResourceVersion.parse(version.text) == version


@given(
    clock_values=st.lists(
        st.integers(min_value=0, max_value=(1 << 48) - 1), min_size=1, max_size=200
    )
)
def test_uuid_generation_stays_monotonic_under_random_clock_movement(
    clock_values: list[int],
) -> None:
    iterator = iter(clock_values)
    generator = IdGenerator(clock=lambda: next(iterator))
    identifiers = [ResourceId("run", generator.raw_now()) for _ in clock_values]

    texts = [identifier.text for identifier in identifiers]
    timestamps = [identifier.unix_millis for identifier in identifiers]
    assert texts == sorted(texts)
    assert len(set(texts)) == len(texts)
    assert timestamps == sorted(timestamps)
