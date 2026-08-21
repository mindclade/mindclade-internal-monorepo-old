# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Hermetic validator for the fail-closed JSON Schema subset used by config contracts."""

from __future__ import annotations

import datetime as dt
import json
import math
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

_SUPPORTED_SCHEMA_KEYWORDS = frozenset(
    {
        "$defs",
        "$id",
        "$ref",
        "$schema",
        "additionalProperties",
        "allOf",
        "anyOf",
        "const",
        "description",
        "else",
        "enum",
        "exclusiveMinimum",
        "format",
        "if",
        "items",
        "maxItems",
        "maxLength",
        "maxProperties",
        "maximum",
        "minItems",
        "minLength",
        "minProperties",
        "minimum",
        "not",
        "oneOf",
        "pattern",
        "properties",
        "required",
        "then",
        "title",
        "type",
        "uniqueItems",
    }
)
_SUPPORTED_TYPES = frozenset({"array", "boolean", "integer", "null", "number", "object", "string"})
_ANNOTATION_KEYWORDS = frozenset({"$id", "$schema", "description", "title"})


@dataclass(frozen=True, order=True)
class ContractError:
    path: str
    message: str


def _matches_type(value: Any, expected: str) -> bool:
    return {
        "array": isinstance(value, list),
        "boolean": isinstance(value, bool),
        "integer": isinstance(value, int) and not isinstance(value, bool),
        "null": value is None,
        "number": isinstance(value, float | int) and not isinstance(value, bool),
        "object": isinstance(value, dict),
        "string": isinstance(value, str),
    }.get(expected, False)


def _resolve(root: dict[str, Any], reference: str) -> dict[str, Any]:
    if not reference.startswith("#/"):
        raise ValueError(f"only local JSON Schema references are supported: {reference}")
    value: Any = root
    for part in reference[2:].split("/"):
        part = part.replace("~1", "/").replace("~0", "~")
        if not isinstance(value, dict) or part not in value:
            raise ValueError(f"unresolvable JSON Schema reference: {reference}")
        value = value[part]
    if not isinstance(value, dict):
        raise ValueError(f"JSON Schema reference is not an object: {reference}")
    return value


def validate_schema_subset(
    schema: dict[str, Any], *, root: dict[str, Any] | None = None, path: str = "$"
) -> tuple[str, ...]:
    """Reject schema features this hermetic validator cannot enforce.

    Silently ignoring a new JSON Schema keyword would turn a repository gate into an allow-open
    parser. This check makes extending the contract language an explicit code-and-test change.
    """

    root = schema if root is None else root
    errors: list[str] = []
    unknown = set(schema) - _SUPPORTED_SCHEMA_KEYWORDS
    for keyword in sorted(unknown):
        errors.append(f"{path}: unsupported JSON Schema keyword {keyword!r}")

    raw_type = schema.get("type")
    if raw_type is not None:
        if isinstance(raw_type, str):
            types = [raw_type]
        elif (
            isinstance(raw_type, list)
            and raw_type
            and all(isinstance(item, str) for item in raw_type)
        ):
            types = raw_type
        else:
            errors.append(f"{path}.type: must be one type or a non-empty list of types")
            types = []
        invalid = sorted(set(types) - _SUPPORTED_TYPES)
        if invalid:
            errors.append(f"{path}.type: unsupported types {invalid}")
        if len(set(types)) != len(types):
            errors.append(f"{path}.type: types must be unique")

    reference = schema.get("$ref")
    if reference is not None:
        if not isinstance(reference, str):
            errors.append(f"{path}.$ref: must be a string")
        else:
            try:
                _resolve(root, reference)
            except ValueError as exc:
                errors.append(f"{path}.$ref: {exc}")

    pattern = schema.get("pattern")
    if pattern is not None:
        if not isinstance(pattern, str):
            errors.append(f"{path}.pattern: must be a string")
        else:
            try:
                re.compile(pattern)
            except re.error as exc:
                errors.append(f"{path}.pattern: invalid regular expression: {exc}")
    if "format" in schema and schema.get("format") != "date-time":
        errors.append(f"{path}.format: only date-time is supported")

    for keyword in ("properties", "$defs"):
        children = schema.get(keyword)
        if children is None:
            continue
        if not isinstance(children, dict):
            errors.append(f"{path}.{keyword}: must be an object")
            continue
        for name, child in sorted(children.items()):
            if not isinstance(child, dict):
                errors.append(f"{path}.{keyword}.{name}: schema must be an object")
                continue
            errors.extend(validate_schema_subset(child, root=root, path=f"{path}.{keyword}.{name}"))

    required = schema.get("required")
    if required is not None:
        if not isinstance(required, list) or not all(isinstance(item, str) for item in required):
            errors.append(f"{path}.required: must be a list of strings")
        elif len(set(required)) != len(required):
            errors.append(f"{path}.required: entries must be unique")

    for keyword in ("additionalProperties", "items", "not", "if", "then", "else"):
        child = schema.get(keyword)
        if child is None:
            continue
        if keyword == "additionalProperties" and isinstance(child, bool):
            continue
        if not isinstance(child, dict):
            errors.append(f"{path}.{keyword}: must be a schema object")
            continue
        errors.extend(validate_schema_subset(child, root=root, path=f"{path}.{keyword}"))

    for keyword in ("allOf", "anyOf", "oneOf"):
        children = schema.get(keyword)
        if children is None:
            continue
        if not isinstance(children, list) or not children:
            errors.append(f"{path}.{keyword}: must be a non-empty list of schemas")
            continue
        for index, child in enumerate(children):
            if not isinstance(child, dict):
                errors.append(f"{path}.{keyword}[{index}]: schema must be an object")
                continue
            errors.extend(
                validate_schema_subset(child, root=root, path=f"{path}.{keyword}[{index}]")
            )
    return tuple(sorted(set(errors)))


def validate(
    instance: Any,
    schema: dict[str, Any],
    *,
    root: dict[str, Any] | None = None,
    path: str = "$",
) -> tuple[ContractError, ...]:
    """Validate without defaulting or coercion."""

    root = schema if root is None else root
    if "$ref" in schema:
        reference_errors = list(
            validate(instance, _resolve(root, str(schema["$ref"])), root=root, path=path)
        )
        siblings = {
            key: value
            for key, value in schema.items()
            if key not in _ANNOTATION_KEYWORDS | {"$ref"}
        }
        if siblings:
            reference_errors.extend(validate(instance, siblings, root=root, path=path))
        return tuple(sorted(set(reference_errors)))
    errors: list[ContractError] = []
    for branch in schema.get("allOf") or []:
        errors.extend(validate(instance, branch, root=root, path=path))
    any_of = schema.get("anyOf") or []
    if any_of and not any(
        not validate(instance, branch, root=root, path=path) for branch in any_of
    ):
        errors.append(ContractError(path, "must satisfy at least one anyOf branch"))
    one_of = schema.get("oneOf") or []
    if (
        one_of
        and sum(not validate(instance, branch, root=root, path=path) for branch in one_of) != 1
    ):
        errors.append(ContractError(path, "must satisfy exactly one oneOf branch"))
    if isinstance(schema.get("not"), dict) and not validate(
        instance, schema["not"], root=root, path=path
    ):
        errors.append(ContractError(path, "matches a forbidden schema"))
    condition = schema.get("if")
    if isinstance(condition, dict):
        selected = (
            schema.get("then")
            if not validate(instance, condition, root=root, path=path)
            else schema.get("else")
        )
        if isinstance(selected, dict):
            errors.extend(validate(instance, selected, root=root, path=path))
    if "const" in schema and instance != schema["const"]:
        errors.append(ContractError(path, f"must equal {schema['const']!r}"))
    if "enum" in schema and instance not in schema["enum"]:
        errors.append(ContractError(path, "is outside the allowed enum"))
    raw_type = schema.get("type")
    expected_types = [raw_type] if isinstance(raw_type, str) else list(raw_type or [])
    if expected_types and not any(_matches_type(instance, item) for item in expected_types):
        return (ContractError(path, f"must have type {' or '.join(expected_types)}"),)

    if isinstance(instance, dict):
        required = schema.get("required") or []
        for name in sorted(set(required) - set(instance)):
            errors.append(ContractError(path, f"missing required property {name!r}"))
        properties = schema.get("properties") or {}
        if not isinstance(properties, dict):
            raise ValueError(f"{path}: properties must be an object")
        extra = set(instance) - set(properties)
        additional = schema.get("additionalProperties", True)
        if additional is False:
            for name in sorted(extra):
                errors.append(ContractError(f"{path}.{name}", "additional property is forbidden"))
        elif isinstance(additional, dict):
            for name in sorted(extra):
                errors.extend(
                    validate(instance[name], additional, root=root, path=f"{path}.{name}")
                )
        for name in sorted(set(instance) & set(properties)):
            errors.extend(
                validate(instance[name], properties[name], root=root, path=f"{path}.{name}")
            )
        count = len(instance)
        if "minProperties" in schema and count < schema["minProperties"]:
            errors.append(ContractError(path, "has too few properties"))
        if "maxProperties" in schema and count > schema["maxProperties"]:
            errors.append(ContractError(path, "has too many properties"))

    if isinstance(instance, list):
        count = len(instance)
        if "minItems" in schema and count < schema["minItems"]:
            errors.append(ContractError(path, "has too few items"))
        if "maxItems" in schema and count > schema["maxItems"]:
            errors.append(ContractError(path, "has too many items"))
        if schema.get("uniqueItems"):
            canonical = [
                json.dumps(item, sort_keys=True, separators=(",", ":")) for item in instance
            ]
            if len(set(canonical)) != len(canonical):
                errors.append(ContractError(path, "items must be unique"))
        item_schema = schema.get("items")
        if isinstance(item_schema, dict):
            for index, item in enumerate(instance):
                errors.extend(validate(item, item_schema, root=root, path=f"{path}[{index}]"))

    if isinstance(instance, str):
        if "minLength" in schema and len(instance) < schema["minLength"]:
            errors.append(ContractError(path, "is shorter than minLength"))
        if "maxLength" in schema and len(instance) > schema["maxLength"]:
            errors.append(ContractError(path, "is longer than maxLength"))
        if "pattern" in schema and re.search(str(schema["pattern"]), instance) is None:
            errors.append(ContractError(path, "does not match the required pattern"))
        if schema.get("format") == "date-time":
            try:
                parsed = dt.datetime.fromisoformat(instance.replace("Z", "+00:00"))
                if parsed.tzinfo is None or parsed.utcoffset() is None:
                    raise ValueError("timezone missing")
            except ValueError:
                errors.append(ContractError(path, "must be a timezone-aware ISO-8601 timestamp"))

    if isinstance(instance, float | int) and not isinstance(instance, bool):
        if isinstance(instance, float) and not math.isfinite(instance):
            errors.append(ContractError(path, "must be finite"))
        if "minimum" in schema and instance < schema["minimum"]:
            errors.append(ContractError(path, "is below minimum"))
        if "maximum" in schema and instance > schema["maximum"]:
            errors.append(ContractError(path, "is above maximum"))
        if "exclusiveMinimum" in schema and instance <= schema["exclusiveMinimum"]:
            errors.append(ContractError(path, "is not above exclusiveMinimum"))

    return tuple(sorted(set(errors)))


def load_json(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected one JSON object")
    return value


def validate_catalog(root: Path) -> list[str]:
    schema_root = root / "schemas"
    fixture_root = root / "fixtures"
    errors: list[str] = []
    identifiers: set[str] = set()
    schema_paths = sorted(schema_root.glob("*.schema.json"))
    if not schema_paths:
        return ["no configuration schemas found"]
    for schema_path in schema_paths:
        name = schema_path.name.removesuffix(".schema.json")
        fixture_path = fixture_root / f"{name}.valid.json"
        try:
            schema = load_json(schema_path)
            errors.extend(f"{schema_path}: {failure}" for failure in validate_schema_subset(schema))
            if schema.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
                errors.append(f"{schema_path}: must use JSON Schema draft 2020-12")
            identifier = str(schema.get("$id", ""))
            if (
                not identifier.startswith("https://mindclade.com/contracts/")
                or identifier in identifiers
            ):
                errors.append(f"{schema_path}: $id must be unique and Mindclade-owned")
            identifiers.add(identifier)
            if schema.get("type") != "object" or schema.get("additionalProperties") is not False:
                errors.append(f"{schema_path}: root must be a closed object")
            if not fixture_path.is_file():
                errors.append(f"{fixture_path}: canonical valid fixture is missing")
                continue
            fixture = load_json(fixture_path)
            errors.extend(
                f"{fixture_path}: {failure.path} {failure.message}"
                for failure in validate(fixture, schema)
            )
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            errors.append(f"{schema_path}: {exc}")
    return errors
