# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Validate the AI-gateway admission OpenAPI projection and mapping."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, cast

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
if str(REPOSITORY_ROOT) not in sys.path:
    sys.path.insert(0, str(REPOSITORY_ROOT))

from configs.contract_validation import load_json, validate, validate_schema_subset  # noqa: E402


def load_json_yaml(path: Path) -> dict[str, Any]:
    payload = "\n".join(
        line
        for line in path.read_text(encoding="utf-8").splitlines()
        if not line.startswith("#") and line.strip() != "---"
    ).strip()
    value = json.loads(payload)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected one object")
    return value


def _objects(value: object) -> dict[str, Any]:
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


def _items(value: object) -> list[Any]:
    return cast("list[Any]", value) if isinstance(value, list) else []


def _dereference(schema: dict[str, Any], root: dict[str, Any]) -> dict[str, Any]:
    reference = schema.get("$ref")
    if isinstance(reference, str) and reference.startswith("#/"):
        value: object = root
        for part in reference[2:].split("/"):
            value = _objects(value).get(part.replace("~1", "/").replace("~0", "~"))
        if isinstance(value, dict):
            return _dereference(value, root)
    return {
        key: (
            _dereference(value, root)
            if isinstance(value, dict)
            else [_dereference(item, root) if isinstance(item, dict) else item for item in value]
            if isinstance(value, list)
            else value
        )
        for key, value in schema.items()
    }


def validate_contracts(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        components = load_json_yaml(root / "components/admission.yaml")
        mapping_root = root.parent / "mappings"
        mapping_schema = load_json(mapping_root / "admission.schema.json")
        mapping = load_json_yaml(mapping_root / "admission.yaml")
        public = (root / "public.openapi.yaml").read_text(encoding="utf-8")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return [str(exc)]

    schemas = _objects(components.get("schemas"))
    required_schemas = {
        "CreateGatewayReservationRequest",
        "CommitGatewayReservationRequest",
        "ReleaseGatewayReservationRequest",
        "GatewayReservation",
        "GatewayReservationDecision",
        "GatewayRoute",
        "GatewayQuota",
    }
    if not required_schemas.issubset(schemas):
        errors.append(
            f"admission OpenAPI schemas missing {sorted(required_schemas - set(schemas))}"
        )
    for name, raw_schema in sorted(schemas.items()):
        if not isinstance(raw_schema, dict):
            errors.append(f"components/admission.yaml {name}: schema must be an object")
            continue
        errors.extend(
            f"components/admission.yaml {name}: {failure}"
            for failure in validate_schema_subset(raw_schema, root=components)
        )
        expanded = _dereference(raw_schema, components)
        if expanded.get("type") == "object" and expanded.get("additionalProperties") is not False:
            errors.append(f"components/admission.yaml {name}: object schema must be closed")

    fixtures = {
        "create.valid.json": "CreateGatewayReservationRequest",
        "commit.valid.json": "CommitGatewayReservationRequest",
        "release.valid.json": "ReleaseGatewayReservationRequest",
        "decision-reserved.valid.json": "GatewayReservationDecision",
        "decision-committed.valid.json": "GatewayReservationDecision",
    }
    for fixture_name, schema_name in fixtures.items():
        try:
            fixture = load_json(root / "fixtures/admission" / fixture_name)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            errors.append(str(exc))
            continue
        errors.extend(
            f"fixtures/admission/{fixture_name} {failure.path}: {failure.message}"
            for failure in validate(fixture, schemas[schema_name], root=components)
        )

    errors.extend(
        f"admission.schema.json: {failure}" for failure in validate_schema_subset(mapping_schema)
    )
    errors.extend(
        f"admission.yaml {failure.path}: {failure.message}"
        for failure in validate(mapping, mapping_schema)
    )
    rest_mappings = [_objects(item) for item in _items(mapping.get("rest"))]
    operation_ids = [str(item.get("operation_id", "")) for item in rest_mappings]
    if operation_ids != sorted(operation_ids) or len(set(operation_ids)) != len(operation_ids):
        errors.append("admission REST mappings must be operation-sorted and operation-unique")
    for item in rest_mappings:
        operation_id = str(item.get("operation_id", ""))
        path = str(item.get("path", ""))
        request_schema = str(item.get("request_schema", "")).rsplit("/", 1)[-1]
        response_schema = str(item.get("response_schema", "")).rsplit("/", 1)[-1]
        if public.count(f"operationId: {operation_id}") != 1:
            errors.append(f"public OpenAPI must declare operation {operation_id!r} exactly once")
        if public.count(f"  {path}:") != 1:
            errors.append(f"public OpenAPI must declare path {path!r} exactly once")
        for schema_name in (request_schema, response_schema):
            if schema_name not in schemas:
                errors.append(f"REST mapping references unknown admission schema {schema_name!r}")
            if public.count(f"    {schema_name}:") != 1:
                errors.append(f"public OpenAPI must export schema {schema_name!r} exactly once")

    if str(_objects(mapping.get("wire_authority")).get("rest")) != (
        "protocols/openapi/components/admission.yaml"
    ):
        errors.append("admission mapping does not name the canonical OpenAPI component")

    reservation_properties = _objects(schemas.get("GatewayReservation", {})).get("properties")
    exposed = set(_objects(reservation_properties))
    forbidden = {"idempotency", "idempotency_key", "request_digest", "subject"}
    if exposed & forbidden:
        errors.append(
            f"reservation response exposes ownership secrets {sorted(exposed & forbidden)}"
        )

    event_root = root.parent / "events/schemas/admission/v1"
    try:
        reservation_event = load_json(event_root / "reservation-event.schema.json")
        entitlement_event = load_json(event_root / "entitlement-event.schema.json")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        errors.append(str(exc))
    else:
        openapi_route = _dereference(_objects(schemas.get("GatewayRoute")), components)
        openapi_quota = _dereference(_objects(schemas.get("GatewayQuota")), components)
        for label, event_schema in (
            ("reservation", reservation_event),
            ("entitlement", entitlement_event),
        ):
            definitions = _objects(event_schema.get("$defs"))
            event_route = _dereference(_objects(definitions.get("GatewayRoute")), event_schema)
            event_quota = _dereference(
                _objects(definitions.get("SingleRequestQuota")), event_schema
            )
            if event_route != openapi_route:
                errors.append(f"{label} event route projection differs from OpenAPI")
            if event_quota != openapi_quota:
                errors.append(f"{label} event single-request quota differs from OpenAPI")
    return sorted(set(errors))


def main() -> int:
    root = Path(__file__).resolve().parent
    errors = validate_contracts(root)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print("admission OpenAPI contract validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
