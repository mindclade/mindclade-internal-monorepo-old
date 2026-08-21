# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
from pathlib import Path

from configs.contract_validation import load_json, validate
from protocols.events.validate_contracts import validate_contracts

ROOT = Path(__file__).resolve().parents[1]


def test_catalog_schemas_fixtures_mappings_and_asyncapi_are_current() -> None:
    assert validate_contracts(ROOT) == []


def test_event_payloads_reject_unknown_fields() -> None:
    for name in ("budget-event", "entitlement-event", "reservation-event"):
        schema = load_json(ROOT / f"schemas/admission/v1/{name}.schema.json")
        fixture = load_json(ROOT / f"fixtures/admission/v1/{name}.valid.json")
        fixture["unreviewed_field"] = "forbidden"
        assert any(error.path == "$.unreviewed_field" for error in validate(fixture, schema))


def test_reservation_lifecycle_shape_is_state_bound() -> None:
    schema = load_json(ROOT / "schemas/admission/v1/reservation-event.schema.json")
    reserved = load_json(ROOT / "fixtures/admission/v1/reservation-event.valid.json")
    invalid = copy.deepcopy(reserved)
    invalid["actual"] = {"requests": 1}
    assert validate(invalid, schema)

    committed = copy.deepcopy(reserved)
    committed["state"] = "committed"
    committed["finalized_at"] = "2026-08-21T09:00:30Z"
    committed["resource_version"] = "rv1:2:sha256:" + "e" * 64
    assert validate(committed, schema)
    committed["actual"] = {"requests": 1}
    assert validate(committed, schema) == ()


def test_gateway_quota_is_integer_bounded_and_single_request() -> None:
    schema = load_json(ROOT / "schemas/admission/v1/entitlement-event.schema.json")
    fixture = load_json(ROOT / "fixtures/admission/v1/entitlement-event.valid.json")
    fixture["maximum_request"]["requests"] = 2
    assert validate(fixture, schema)
    fixture["maximum_request"]["requests"] = 1
    fixture["maximum_request"]["input_tokens"] = 1000000000000000001
    assert validate(fixture, schema)
