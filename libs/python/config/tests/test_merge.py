# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from types import MappingProxyType

import pytest

from libs.python.config import (
    MergeError,
    OverrideError,
    apply_override,
    deep_merge,
    deep_merge_many,
)
from libs.python.errors import Code, code_of


def test_deep_merge_recurses_without_mutating_either_input() -> None:
    base = {"model": {"layers": 2, "widths": [32, 64]}, "runtime": {"precision": "bf16"}}
    overlay = {"model": {"layers": 4}}

    merged = deep_merge(base, overlay)

    assert merged == {
        "model": {"layers": 4, "widths": [32, 64]},
        "runtime": {"precision": "bf16"},
    }
    model = merged["model"]
    assert isinstance(model, dict)
    widths = model["widths"]
    assert isinstance(widths, list)
    widths.append(128)
    base_model = base["model"]
    assert isinstance(base_model, dict)
    assert base_model["widths"] == [32, 64]
    assert overlay == {"model": {"layers": 4}}


def test_deep_merge_accepts_read_only_mappings() -> None:
    base = MappingProxyType({"model": MappingProxyType({"layers": 2})})
    assert deep_merge(base, {"model": {"width": 64}}) == {"model": {"layers": 2, "width": 64}}


def test_deep_merge_many_matches_sequential_merging_without_aliasing() -> None:
    base = {"model": {"layers": 2, "widths": [32, 64]}, "runtime": {"seed": 7}}
    overlays = ({"model": {"layers": 4}}, {"runtime": {"precision": "bf16"}})
    expected = base
    for overlay in overlays:
        expected = deep_merge(expected, overlay)

    merged = deep_merge_many(base, overlays)

    assert merged == expected
    merged_model = merged["model"]
    base_model = base["model"]
    assert isinstance(merged_model, dict)
    assert isinstance(base_model, dict)
    merged_widths = merged_model["widths"]
    assert isinstance(merged_widths, list)
    merged_widths.append(128)
    assert base_model["widths"] == [32, 64]


def test_type_change_reports_the_complete_path_as_an_invalid_argument() -> None:
    with pytest.raises(MergeError, match=r"model\.layers") as caught:
        deep_merge({"model": {"layers": 2}}, {"model": {"layers": "two"}})
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_type_change_can_be_explicitly_allowed() -> None:
    assert deep_merge({"layers": 2}, {"layers": "two"}, reject_type_changes=False) == {
        "layers": "two"
    }


@pytest.mark.parametrize("path", ["", ".model", "model.", "model..layers", "model space.x"])
def test_override_path_rejects_ambiguous_names(path: str) -> None:
    with pytest.raises(OverrideError, match="path"):
        apply_override({}, f"{path}=1")


def test_override_creates_missing_mappings_and_parses_json_values() -> None:
    target: dict[str, object] = {}
    path, value = apply_override(target, 'model.options={"compile":true}')
    assert path == "model.options"
    assert value == {"compile": True}
    assert target == {"model": {"options": {"compile": True}}}


def test_override_rejects_scalar_traversal_and_type_replacement() -> None:
    with pytest.raises(OverrideError, match="traverses a scalar"):
        apply_override({"model": 1}, "model.layers=2")
    with pytest.raises(OverrideError, match="changes type"):
        apply_override({"model": {"layers": 2}}, 'model.layers="two"')


def test_configuration_helpers_reject_invalid_public_inputs() -> None:
    with pytest.raises(MergeError, match="mappings"):
        deep_merge([], {})  # type: ignore[arg-type]
    with pytest.raises(MergeError, match="keys"):
        deep_merge({1: "value"}, {})  # type: ignore[dict-item]
    with pytest.raises(MergeError, match="boolean"):
        deep_merge({}, {}, reject_type_changes=1)  # type: ignore[arg-type]
    with pytest.raises(OverrideError, match="mutable mapping"):
        apply_override([], "model.layers=2")  # type: ignore[arg-type]
