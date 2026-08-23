# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
from pathlib import Path

import pytest

from configs.contract_validation import load_json, validate
from protocols.openapi.validate_contracts import (
    DocumentScanError,
    _children,
    _dereference,
    _operation_ids,
    _safe_validate,
    load_json_yaml,
    scan_document,
    validate_admin_surface,
    validate_contracts,
)

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


def test_operation_scan_is_structural_not_textual(tmp_path: Path) -> None:
    """A comment, a prefix rename, and free text must not stand in for a declared operation.

    The three cases below each satisfied the substring counter this check replaced, which is how a
    gate can report a published operation that no longer exists anywhere in the document.
    """

    document = tmp_path / "spec.yaml"
    document.write_text(
        "paths:\n"
        "  /v1/things:\n"
        "    post:\n"
        "      operationId: realOperation\n"
        "      summary: replaces operationId: freeTextOperation\n"
        "      # operationId: commentedOperation\n"
        "      description: |\n"
        "        operationId: proseOperation\n"
        "components:\n"
        "  schemas:\n"
        "    Thing:\n"
        "      type: object\n",
        encoding="utf-8",
    )
    entries, _ = scan_document(document)

    assert _operation_ids(entries) == ["realOperation"]
    assert _children(entries, ("paths",)) == ["/v1/things"]
    assert _children(entries, ("components", "schemas")) == ["Thing"]


def test_operation_scan_counts_exact_names_not_prefixes(tmp_path: Path) -> None:
    document = tmp_path / "spec.yaml"
    document.write_text(
        "paths:\n  /v1/things:\n    post:\n      operationId: createThingX\n",
        encoding="utf-8",
    )
    entries, _ = scan_document(document)

    assert _operation_ids(entries).count("createThing") == 0


def test_path_keys_may_contain_an_inner_colon(tmp_path: Path) -> None:
    document = tmp_path / "spec.yaml"
    document.write_text(
        "paths:\n  /v1/runs/{runId}:cancel:\n    post:\n      operationId: cancelRun\n",
        encoding="utf-8",
    )
    entries, _ = scan_document(document)

    assert _children(entries, ("paths",)) == ["/v1/runs/{runId}:cancel"]


def test_dereference_preserves_keywords_written_beside_a_reference() -> None:
    """A constraint next to a `$ref` used to be discarded, so parity comparison could not see it."""

    root = {"$defs": {"Name": {"type": "string", "maxLength": 8}}}
    plain = _dereference({"$ref": "#/$defs/Name"}, root)
    constrained = _dereference({"$ref": "#/$defs/Name", "maxLength": 9999}, root)

    assert plain == {"type": "string", "maxLength": 8}
    assert constrained == {"type": "string", "maxLength": 9999}
    assert plain != constrained


def test_unresolvable_reference_is_reported_rather_than_raised() -> None:
    failures = _safe_validate({}, {"$ref": "#/schemas/Missing"}, {"schemas": {}}, "probe")

    assert failures and "could not run" in failures[0]


def test_document_scan_bounds_are_enforced(tmp_path: Path) -> None:
    oversized = tmp_path / "oversized.yaml"
    oversized.write_text("key: " + "v" * 8192 + "\n", encoding="utf-8")
    with pytest.raises(DocumentScanError):
        scan_document(oversized)

    deep = tmp_path / "deep.yaml"
    deep.write_text(
        "".join(f"{' ' * (2 * level)}k{level}:\n" for level in range(64)), encoding="utf-8"
    )
    with pytest.raises(DocumentScanError):
        scan_document(deep)


def test_admin_surface_is_validated_not_merely_declared() -> None:
    """`admin.openapi.yaml` had no reader at all; this pins that it now has one."""

    assert validate_admin_surface(ROOT) == []
