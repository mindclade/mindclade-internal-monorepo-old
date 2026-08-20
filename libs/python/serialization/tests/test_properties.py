# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import json

from hypothesis import given
from hypothesis import strategies as st

from libs.python.serialization import canonical_json_bytes

TEXT = st.text(alphabet=st.characters(max_codepoint=0xD7FF), max_size=64)
SCALAR = st.one_of(st.none(), st.booleans(), st.integers(), TEXT)
JSON_VALUE = st.recursive(
    SCALAR,
    lambda children: st.one_of(
        st.lists(children, max_size=5),
        st.dictionaries(TEXT, children, max_size=5),
    ),
    max_leaves=30,
)
DOCUMENT = st.dictionaries(TEXT, JSON_VALUE, max_size=8)


@given(document=DOCUMENT)
def test_canonical_serialization_is_deterministic_and_round_trips(
    document: dict[str, object],
) -> None:
    encoded = canonical_json_bytes(document)

    assert encoded == canonical_json_bytes(document)
    assert json.loads(encoded) == document


@given(document=DOCUMENT)
def test_mapping_insertion_order_does_not_change_canonical_bytes(
    document: dict[str, object],
) -> None:
    reversed_document = dict(reversed(tuple(document.items())))
    assert canonical_json_bytes(document) == canonical_json_bytes(reversed_document)
