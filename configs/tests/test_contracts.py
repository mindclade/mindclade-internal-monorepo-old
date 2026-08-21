# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
from pathlib import Path

from configs.contract_validation import (
    load_json,
    validate,
    validate_catalog,
    validate_schema_subset,
)

ROOT = Path(__file__).resolve().parents[1]


def test_every_schema_has_a_valid_fail_closed_fixture() -> None:
    assert validate_catalog(ROOT) == []


def test_model_target_approval_requires_evidence() -> None:
    schema = load_json(ROOT / "schemas/model-target-card.schema.json")
    fixture = load_json(ROOT / "fixtures/model-target-card.valid.json")
    approved = copy.deepcopy(fixture)
    approved["activation"]["state"] = "approved"
    assert validate(approved, schema)


def test_unknown_release_evidence_is_rejected() -> None:
    schema = load_json(ROOT / "schemas/release.schema.json")
    fixture = load_json(ROOT / "fixtures/release.valid.json")
    fixture["unreviewedEvidence"] = "sha256:" + "0" * 64
    assert any(error.path == "$.unreviewedEvidence" for error in validate(fixture, schema))


def test_timestamp_without_timezone_is_rejected() -> None:
    schema = load_json(ROOT / "schemas/run.schema.json")
    fixture = load_json(ROOT / "fixtures/run.valid.json")
    fixture["createdAt"] = "2026-08-20T00:00:00"
    assert any("timezone-aware" in error.message for error in validate(fixture, schema))


def test_unsupported_schema_keyword_fails_closed() -> None:
    errors = validate_schema_subset(
        {"type": "object", "unevaluatedProperties": False, "properties": {}}
    )
    assert errors == ("$: unsupported JSON Schema keyword 'unevaluatedProperties'",)


def test_ref_siblings_are_enforced() -> None:
    schema = {
        "$defs": {"identifier": {"type": "string"}},
        "$ref": "#/$defs/identifier",
        "pattern": "^[a-z]+$",
    }
    assert validate("safe", schema) == ()
    assert any("pattern" in error.message for error in validate("NOT-SAFE", schema))
