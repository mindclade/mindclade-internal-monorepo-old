# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from pathlib import Path

import pytest

from libs.python.config import RequiredField, ValidationError, resolve, validate_required


def test_resolved_digest_stable_and_override_recorded(tmp_path: Path) -> None:
    a = tmp_path / "base.toml"
    b = tmp_path / "model.toml"
    a.write_text("[runtime]\nprecision='bf16'\n[model]\nlayers=2\n", encoding="utf-8")
    b.write_text("[model]\nwidth=64\n", encoding="utf-8")
    x = resolve([a, b], overrides=["model.layers=4"])
    y = resolve([a, b], overrides=["model.layers=4"])
    assert x.digest == y.digest and x.value["model"]["layers"] == 4
    assert x.overrides[0].path == "model.layers"


def test_boolean_does_not_satisfy_numeric_schema_field() -> None:
    with pytest.raises(ValidationError, match="has type bool"):
        validate_required({"model": {"layers": True}}, [RequiredField("model.layers", int)])


def test_required_validation_rejects_invalid_public_inputs() -> None:
    with pytest.raises(ValidationError, match="must be a mapping"):
        validate_required([], [])  # type: ignore[arg-type]
    with pytest.raises(ValidationError, match="RequiredField"):
        validate_required({}, [object()])  # type: ignore[list-item]
