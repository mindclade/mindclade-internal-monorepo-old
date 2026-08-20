# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import copy
import json

from hypothesis import given
from hypothesis import strategies as st

from libs.python.config import apply_override, deep_merge, deep_merge_many

KEY = st.from_regex(r"[A-Za-z_][A-Za-z0-9_-]{0,15}", fullmatch=True)
TEXT = st.text(alphabet=st.characters(max_codepoint=0xD7FF), max_size=32)
SCALAR = st.one_of(st.none(), st.booleans(), st.integers(), TEXT)
VALUE = st.recursive(
    SCALAR,
    lambda children: st.one_of(
        st.lists(children, max_size=4),
        st.dictionaries(KEY, children, max_size=4),
    ),
    max_leaves=20,
)
CONFIG = st.dictionaries(KEY, VALUE, max_size=6)


@given(base=CONFIG, overlays=st.lists(CONFIG, max_size=8))
def test_batched_deep_merge_matches_sequential_merge_and_preserves_inputs(
    base: dict[str, object], overlays: list[dict[str, object]]
) -> None:
    original_base = copy.deepcopy(base)
    original_overlays = copy.deepcopy(overlays)
    sequential = base
    for overlay in overlays:
        sequential = deep_merge(sequential, overlay, reject_type_changes=False)

    assert deep_merge_many(base, overlays, reject_type_changes=False) == sequential
    assert base == original_base
    assert overlays == original_overlays


@given(parts=st.lists(KEY, min_size=1, max_size=5), value=SCALAR)
def test_override_sets_exact_path_and_is_idempotent(parts: list[str], value: object) -> None:
    expression = f"{'.'.join(parts)}={json.dumps(value, ensure_ascii=False)}"
    target: dict[str, object] = {}

    path, parsed = apply_override(target, expression)
    apply_override(target, expression)

    cursor: object = target
    for part in parts:
        assert isinstance(cursor, dict)
        cursor = cursor[part]
    assert path == ".".join(parts)
    assert parsed == value
    assert cursor == value
