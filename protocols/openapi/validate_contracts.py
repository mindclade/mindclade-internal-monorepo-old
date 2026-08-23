# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate the AI-gateway admission OpenAPI projection and mapping."""

from __future__ import annotations

import json
import re
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


# `public.openapi.yaml`, `admin.openapi.yaml`, and `components/evidence.yaml` are block YAML, not
# the JSON-compatible subset `load_json_yaml` accepts, so this file used to interrogate them by
# counting substrings in the raw text. That check was vacuous in three demonstrated ways: renaming
# `createGatewayReservation` to `createGatewayReservationX` still contained the original as a
# prefix; commenting the operation out left `# operationId: createGatewayReservation` for the
# counter to find; and writing the name into a `summary:` string satisfied it with no operation
# present at all. Structure has to come from a parse, so the scanner below recovers block-mapping
# key paths and nothing else. Every bound is explicit because an unbounded parser over a contract
# document is exactly the shape this repository forbids.
_MAX_DOCUMENT_LINES = 20_000
_MAX_LINE_LENGTH = 4_096
_MAX_MAPPING_DEPTH = 32

# A plain block key ends at the first colon followed by whitespace or end-of-line. The lookahead
# matters: OpenAPI path keys such as `/v1/runs/{runId}:cancel` legally contain an inner colon.
_KEY_LINE = re.compile(r"^(?P<key>'[^']*'|\"[^\"]*\"|.*?)\s*:(?=\s|$)(?:\s+(?P<value>.*?))?\s*$")
_BLOCK_SCALAR = re.compile(r"^[|>][+-]?\d*$")
_REFERENCE = re.compile(r"\$ref:\s*['\"]?([^'\"}\s,]+)")

KeyPath = tuple[str, ...]


def _strip_comment(text: str) -> str:
    """Drop a trailing YAML comment without cutting inside a quoted scalar."""

    quote = ""
    for index, character in enumerate(text):
        if quote:
            if character == quote:
                quote = ""
        elif character in "'\"":
            quote = character
        elif character == "#" and (index == 0 or text[index - 1].isspace()):
            return text[:index].rstrip()
    return text.rstrip()


def _unquote(key: str) -> str:
    if len(key) >= 2 and key[0] == key[-1] and key[0] in "'\"":
        return key[1:-1]
    return key


class DocumentScanError(ValueError):
    """A contract document exceeded a scanner bound or broke the supported YAML subset."""


def scan_document(path: Path) -> tuple[list[tuple[KeyPath, str]], list[str]]:
    """Return the block-mapping key paths and the comment-stripped lines of one document.

    Sequence entries and block scalars are skipped wholesale: no key this validator asks about
    lives inside either, and descending into them is how free text gets mistaken for structure.
    """

    raw_lines = path.read_text(encoding="utf-8").splitlines()
    if len(raw_lines) > _MAX_DOCUMENT_LINES:
        raise DocumentScanError(f"{path}: exceeds {_MAX_DOCUMENT_LINES} lines")

    entries: list[tuple[KeyPath, str]] = []
    content: list[str] = []
    stack: list[tuple[int, str]] = []
    skip_deeper_than: int | None = None
    # Sequence bodies are still structured YAML, so their lines stay available to the reference
    # check even though they contribute no key path. Block-scalar bodies are prose and must not be
    # read as structure at all — that is how a `description:` came to satisfy an operation check.
    skip_is_prose = False

    for number, raw in enumerate(raw_lines, start=1):
        if len(raw) > _MAX_LINE_LENGTH:
            raise DocumentScanError(f"{path}:{number}: exceeds {_MAX_LINE_LENGTH} characters")
        indent = len(raw) - len(raw.lstrip(" "))
        if "\t" in raw[:indent]:
            raise DocumentScanError(f"{path}:{number}: tab indentation is not supported")
        stripped = _strip_comment(raw.strip())
        if not stripped or stripped == "---":
            continue
        if skip_deeper_than is not None:
            if indent > skip_deeper_than:
                if not skip_is_prose:
                    content.append(stripped)
                continue
            skip_deeper_than = None
        content.append(stripped)
        if stripped == "-" or stripped.startswith("- "):
            skip_deeper_than = indent
            skip_is_prose = False
            continue
        match = _KEY_LINE.match(stripped)
        if match is None:
            continue
        while stack and stack[-1][0] >= indent:
            stack.pop()
        if len(stack) >= _MAX_MAPPING_DEPTH:
            raise DocumentScanError(f"{path}:{number}: exceeds {_MAX_MAPPING_DEPTH} nesting levels")
        stack.append((indent, _unquote(match.group("key"))))
        value = match.group("value") or ""
        entries.append((tuple(key for _, key in stack), value))
        if _BLOCK_SCALAR.match(value):
            skip_deeper_than = indent
            skip_is_prose = True
    return entries, content


def _children(entries: list[tuple[KeyPath, str]], prefix: KeyPath) -> list[str]:
    """Every key declared directly under `prefix`, in document order and with duplicates kept."""

    depth = len(prefix) + 1
    return [path[-1] for path, _ in entries if len(path) == depth and path[:-1] == prefix]


def _operation_ids(entries: list[tuple[KeyPath, str]]) -> list[str]:
    return [
        value
        for path, value in entries
        if len(path) == 4 and path[0] == "paths" and path[3] == "operationId" and value
    ]


def _dereference(schema: dict[str, Any], root: dict[str, Any], *, depth: int = 0) -> dict[str, Any]:
    """Expand local references so two documents can be compared as values.

    Sibling keywords beside a `$ref` used to be discarded here, which made the event/OpenAPI parity
    comparison blind to any constraint written next to a reference — adding `maxLength` beside
    `{"$ref": "#/$defs/GatewayName"}` on one side only still compared equal, while
    `configs.contract_validation.validate` honours those siblings at runtime. The two now agree.
    """

    if depth > _MAX_MAPPING_DEPTH:
        raise DocumentScanError("JSON Schema reference expansion exceeded its depth bound")
    reference = schema.get("$ref")
    if isinstance(reference, str) and reference.startswith("#/"):
        value: object = root
        for part in reference[2:].split("/"):
            value = _objects(value).get(part.replace("~1", "/").replace("~0", "~"))
        if isinstance(value, dict):
            resolved = _dereference(value, root, depth=depth + 1)
            siblings = {key: item for key, item in schema.items() if key != "$ref"}
            if siblings:
                resolved = {**resolved, **_dereference(siblings, root, depth=depth + 1)}
            return resolved
    return {
        key: (
            _dereference(value, root, depth=depth + 1)
            if isinstance(value, dict)
            else [
                _dereference(item, root, depth=depth + 1) if isinstance(item, dict) else item
                for item in value
            ]
            if isinstance(value, list)
            else value
        )
        for key, value in schema.items()
    }


def _safe_validate(
    instance: Any, schema: dict[str, Any], root: dict[str, Any] | None, label: str
) -> list[str]:
    """Report an unusable schema instead of letting its ValueError escape as a traceback."""

    try:
        return [
            f"{label} {failure.path}: {failure.message}"
            for failure in validate(instance, schema, root=root)
        ]
    except ValueError as exc:
        return [f"{label}: contract validation could not run: {exc}"]


def validate_contracts(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        components = load_json_yaml(root / "components/admission.yaml")
        mapping_root = root.parent / "mappings"
        mapping_schema = load_json(mapping_root / "admission.schema.json")
        mapping = load_json_yaml(mapping_root / "admission.yaml")
        public_entries, _ = scan_document(root / "public.openapi.yaml")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return [str(exc)]

    public_operations = _operation_ids(public_entries)
    public_paths = _children(public_entries, ("paths",))
    public_schemas = _children(public_entries, ("components", "schemas"))
    # A scanner that silently recovers nothing would restore exactly the vacuity it replaced, so
    # an empty projection is a failure rather than a clean run over zero declarations.
    if not (public_operations and public_paths and public_schemas):
        errors.append(
            "public OpenAPI structural scan recovered no operations, paths, or component schemas"
        )

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
    # Schema-level defects are collected separately: validating a fixture against a schema whose
    # `$ref` does not resolve raises out of the shared validator, and this function reported that
    # as a traceback rather than as an error list a caller could act on.
    schema_errors: list[str] = []
    for name, raw_schema in sorted(schemas.items()):
        if not isinstance(raw_schema, dict):
            schema_errors.append(f"components/admission.yaml {name}: schema must be an object")
            continue
        schema_errors.extend(
            f"components/admission.yaml {name}: {failure}"
            for failure in validate_schema_subset(raw_schema, root=components)
        )
        try:
            expanded = _dereference(raw_schema, components)
        except DocumentScanError as exc:
            schema_errors.append(f"components/admission.yaml {name}: {exc}")
            continue
        if expanded.get("type") == "object" and expanded.get("additionalProperties") is not False:
            schema_errors.append(f"components/admission.yaml {name}: object schema must be closed")
    errors.extend(schema_errors)

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
        fixture_schema = schemas.get(schema_name)
        if not isinstance(fixture_schema, dict):
            errors.append(
                f"fixtures/admission/{fixture_name}: schema {schema_name!r} is not declared"
            )
            continue
        errors.extend(
            _safe_validate(
                fixture, fixture_schema, components, f"fixtures/admission/{fixture_name}"
            )
        )

    errors.extend(
        f"admission.schema.json: {failure}" for failure in validate_schema_subset(mapping_schema)
    )
    errors.extend(_safe_validate(mapping, mapping_schema, None, "admission.yaml"))
    rest_mappings = [_objects(item) for item in _items(mapping.get("rest"))]
    operation_ids = [str(item.get("operation_id", "")) for item in rest_mappings]
    if operation_ids != sorted(operation_ids) or len(set(operation_ids)) != len(operation_ids):
        errors.append("admission REST mappings must be operation-sorted and operation-unique")
    for item in rest_mappings:
        operation_id = str(item.get("operation_id", ""))
        path = str(item.get("path", ""))
        request_schema = str(item.get("request_schema", "")).rsplit("/", 1)[-1]
        response_schema = str(item.get("response_schema", "")).rsplit("/", 1)[-1]
        if public_operations.count(operation_id) != 1:
            errors.append(f"public OpenAPI must declare operation {operation_id!r} exactly once")
        if public_paths.count(path) != 1:
            errors.append(f"public OpenAPI must declare path {path!r} exactly once")
        for schema_name in (request_schema, response_schema):
            if schema_name not in schemas:
                errors.append(f"REST mapping references unknown admission schema {schema_name!r}")
            if public_schemas.count(schema_name) != 1:
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
        try:
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
        except DocumentScanError as exc:
            errors.append(f"event/OpenAPI parity comparison could not run: {exc}")

    errors.extend(validate_admin_surface(root))
    return sorted(set(errors))


def validate_admin_surface(root: Path) -> list[str]:
    """Structurally gate `admin.openapi.yaml` and the component it is the only reader of.

    Neither document was opened by any validator, generator, or test: they were listed as Bazel
    data and otherwise unread, so a dangling `$ref` or a duplicated operation could sit in a
    published contract indefinitely. This does not give the admin surface a consumer — it has
    none — but it stops the spec rotting silently while that question is settled.
    """

    errors: list[str] = []
    try:
        admin_entries, admin_lines = scan_document(root / "admin.openapi.yaml")
        evidence_entries, _ = scan_document(root / "components/evidence.yaml")
    except (OSError, ValueError) as exc:
        return [str(exc)]

    admin_operations = _operation_ids(admin_entries)
    admin_paths = _children(admin_entries, ("paths",))
    evidence_schemas = _children(evidence_entries, ("schemas",))
    if not (admin_operations and admin_paths and evidence_schemas):
        return ["admin OpenAPI structural scan recovered no operations, paths, or evidence schemas"]

    for label, declared in (
        ("operation", admin_operations),
        ("path", admin_paths),
        ("evidence schema", evidence_schemas),
    ):
        duplicates = sorted({item for item in declared if declared.count(item) > 1})
        if duplicates:
            errors.append(f"admin OpenAPI declares duplicate {label} entries {duplicates}")

    local_targets = {
        "/".join(path) for path, _ in admin_entries if path and path[0] == "components"
    }
    for line in admin_lines:
        for reference in _REFERENCE.findall(line):
            document, _, pointer = reference.partition("#")
            if not pointer.startswith("/"):
                errors.append(f"admin OpenAPI reference {reference!r} has no local pointer")
            elif document == "./components/evidence.yaml":
                name = pointer.removeprefix("/schemas/")
                if "/" in name or name not in evidence_schemas:
                    errors.append(
                        f"admin OpenAPI references undeclared evidence schema {pointer!r}"
                    )
            elif not document:
                if pointer.lstrip("/") not in local_targets:
                    errors.append(f"admin OpenAPI reference {reference!r} does not resolve")
            else:
                errors.append(f"admin OpenAPI references unknown document {document!r}")
    return errors


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
