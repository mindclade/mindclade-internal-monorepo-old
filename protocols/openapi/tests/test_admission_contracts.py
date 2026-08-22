# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
from pathlib import Path

from configs.contract_validation import load_json, validate
from protocols.openapi.validate_contracts import load_json_yaml, validate_contracts

ROOT = Path(__file__).resolve().parents[1]


def test_admission_openapi_fixtures_mappings_and_event_parity() -> None:
    assert validate_contracts(ROOT) == []


def test_requests_are_closed_and_release_cannot_report_usage() -> None:
    components = load_json_yaml(ROOT / "components/admission.yaml")
    schemas = components["schemas"]
    create = load_json(ROOT / "fixtures/admission/create.valid.json")
    create["unreviewed_field"] = True
    assert validate(create, schemas["CreateGatewayReservationRequest"], root=components)

    release = load_json(ROOT / "fixtures/admission/release.valid.json")
    release["actual"] = {"requests": 1}
    assert validate(release, schemas["ReleaseGatewayReservationRequest"], root=components)


def test_response_redacts_mutation_ownership_material() -> None:
    components = load_json_yaml(ROOT / "components/admission.yaml")
    schema = components["schemas"]["GatewayReservationDecision"]
    decision = load_json(ROOT / "fixtures/admission/decision-reserved.valid.json")
    for field in ("request_digest", "subject", "idempotency_key"):
        candidate = copy.deepcopy(decision)
        candidate["reservation"][field] = "secret"
        assert validate(candidate, schema, root=components)


def test_committed_decision_requires_actual_usage_and_generation_two() -> None:
    components = load_json_yaml(ROOT / "components/admission.yaml")
    schema = components["schemas"]["GatewayReservationDecision"]
    decision = load_json(ROOT / "fixtures/admission/decision-committed.valid.json")
    del decision["reservation"]["actual"]
    assert validate(decision, schema, root=components)
    decision["reservation"]["actual"] = {"requests": 1}
    decision["reservation"]["resource_version"] = "rv1:1:sha256:" + "e" * 64
    assert validate(decision, schema, root=components)
