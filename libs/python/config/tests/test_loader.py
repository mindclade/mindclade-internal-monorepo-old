# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from collections.abc import Iterator
from pathlib import Path

import pytest

from libs.python.config import (
    MAXIMUM_OVERLAYS,
    MAXIMUM_SOURCES,
    RequiredField,
    ResolvedConfig,
    Source,
    ValidationError,
    resolve,
    validate_required,
)
from libs.python.errors import Code, MindcladeError, code_of, public_message_of
from libs.python.identifiers import Digest


def test_resolve_records_ordered_sources_and_applies_layers(tmp_path: Path) -> None:
    base = tmp_path / "base.toml"
    model = tmp_path / "model.toml"
    base.write_text("[runtime]\nprecision='bf16'\n[model]\nlayers=2\n", encoding="utf-8")
    model.write_text("[model]\nwidth=64\n", encoding="utf-8")

    resolved = resolve(
        [base, model],
        overlays=({"runtime": {"compile": True}},),
        overrides=("model.layers=4",),
    )

    assert [source.name for source in resolved.sources] == ["base", "model"]
    assert resolved.value == {
        "model": {"layers": 4, "width": 64},
        "runtime": {"compile": True, "precision": "bf16"},
    }
    assert resolved.overrides[0].path == "model.layers"
    assert resolved.digest == Digest.of(resolved.canonical_bytes()).text


def test_resolved_value_is_a_recursive_snapshot(tmp_path: Path) -> None:
    source = tmp_path / "config.toml"
    source.write_text("[model]\nlayers=[1, 2]\n", encoding="utf-8")
    resolved = resolve([source])

    with pytest.raises(TypeError):
        resolved.value["new"] = "value"  # type: ignore[index]
    with pytest.raises(TypeError):
        resolved.value["model"]["layers"] = ()
    assert resolved.value["model"]["layers"] == (1, 2)


def test_direct_resolved_config_rejects_a_mismatched_digest() -> None:
    with pytest.raises(ValueError, match="digest") as caught:
        ResolvedConfig({}, "sha256:" + "0" * 64, (), ())
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


@pytest.mark.parametrize(
    ("contents", "reason"),
    [
        (b"\xff", "UTF-8 TOML"),
        (b"[broken", "UTF-8 TOML"),
    ],
)
def test_invalid_sources_are_controlled_faults(
    tmp_path: Path, contents: bytes, reason: str
) -> None:
    source = tmp_path / "config.toml"
    source.write_bytes(contents)
    with pytest.raises(ValueError, match=reason) as caught:
        resolve([source])
    assert code_of(caught.value) is Code.INVALID_ARGUMENT


def test_missing_source_does_not_disclose_as_a_public_message(tmp_path: Path) -> None:
    source = tmp_path / "private" / "config.toml"
    with pytest.raises(ValueError) as caught:
        resolve([source])
    assert code_of(caught.value) is Code.INVALID_ARGUMENT
    assert str(source) not in public_message_of(caught.value)


def test_source_count_is_bounded_before_any_file_is_opened(tmp_path: Path) -> None:
    paths = [tmp_path / f"missing-{index}.toml" for index in range(MAXIMUM_SOURCES + 1)]
    with pytest.raises(MindcladeError, match="at most") as caught:
        resolve(paths)
    assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED


def test_resolve_rejects_ambiguous_or_invalid_collection_inputs() -> None:
    with pytest.raises(ValueError, match="sequence of filesystem paths"):
        resolve("config.toml")
    with pytest.raises(ValueError, match="filesystem paths"):
        resolve([object()])  # type: ignore[list-item]
    with pytest.raises(ValueError, match="iterable mappings"):
        resolve([], overlays=1)  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="must be mappings"):
        resolve([], overlays=[object()])  # type: ignore[list-item]
    with pytest.raises(ValueError, match="sequence of expressions"):
        resolve([], overrides="model.layers=2")


def test_overlay_iterables_are_bounded_during_materialization() -> None:
    overlays: Iterator[dict[str, object]] = ({} for _ in range(MAXIMUM_OVERLAYS + 2))
    with pytest.raises(MindcladeError, match="at most") as caught:
        resolve([], overlays=overlays)
    assert code_of(caught.value) is Code.RESOURCE_EXHAUSTED


def test_source_and_schema_records_validate_at_construction() -> None:
    with pytest.raises(ValueError):
        Source("", "config.toml", Digest.of(b"").text)
    with pytest.raises(ValueError):
        Source("base", "config.toml", "not-a-digest")
    with pytest.raises(ValueError):
        RequiredField("model..layers", int)


def test_required_field_validation_reports_all_missing_or_wrong_fields() -> None:
    fields = [RequiredField("model.layers", int), RequiredField("runtime.precision", str)]
    with pytest.raises(ValidationError, match=r"model\.layers.*runtime\.precision"):
        validate_required({"model": {"layers": "two"}}, fields)
