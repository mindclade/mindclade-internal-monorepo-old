#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Derive the checked-in event JSON Schemas from the governed protobuf descriptor surface.

`protocols/events/generated/` was a hand-maintained tree with "generated" in its path and no
generator behind it. Eight byte-identical copies of the event envelope schema were kept in step
by hand, and nothing compared any of them to `mindclade.events.v1.EventEnvelope`. A field added
to the proto and not to the schema, a field dropped, a `required` entry lost, or seven copies
drifting from the eighth all passed.

The derivation chain this module completes is:

    protocols/proto/**.proto
      -> protoc  (//protocols:protobuf_descriptor_set)
      -> protocols/compatibility/protobuf-v1-descriptor.json
           gated byte-for-byte by //protocols:protobuf_governance_test
      -> protocols/events/generated/**.schema.json
           gated by this module's --check

Both links fail closed, so neither end can move without the other. The descriptor surface is the
input rather than the .proto text because parsing protobuf source would be a second, unreviewed
implementation of what protoc already decides; the surface is protoc's own answer, already
compared against the sources on every run of the governance test.

Structure comes from the descriptor. `protocols/mappings/event_proto.yaml` may only REFINE it:
tighten a range, add a pattern, declare which fields are required on the wire. It cannot add a
property the message does not have, contradict a projected JSON type, or widen a protobuf range,
and every refinement must name a field that still exists. That asymmetry is the whole point — a
proto change that the policy has not caught up with is an error here, not a silent disagreement
between a producer and a consumer.

Bounds are mandatory, not advisory. Every string, bytes, repeated, and 64-bit integer projection
must carry an explicit limit, because an unbounded wire field is a parser with no limit. 64-bit
integers additionally may not exceed 2^53-1: these schemas project them as JSON numbers, and a
JSON number past that point does not survive a round trip through an IEEE-754 double.

Usage:

    python3 tools/codegen/generate_jsonschema.py            # write the tree
    python3 tools/codegen/generate_jsonschema.py --check    # fail on drift, write nothing
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from protocols.events.validate_contracts import load_json_yaml  # noqa: E402

POLICY_RELPATH = "protocols/mappings/event_proto.yaml"

JSON_SCHEMA_DIALECT = "https://json-schema.org/draft/2020-12/schema"

# The largest integer an IEEE-754 double represents exactly. These schemas project 64-bit
# protobuf integers as JSON numbers (the checked-in convention, and not canonical proto3 JSON,
# which strings them), so anything above this silently changes value in a JavaScript consumer —
# and sdk/typescript is one.
SAFE_JSON_INTEGER = 2**53 - 1

# Standard base64 with padding, which is how proto3 JSON encodes `bytes`. `contentEncoding` says
# the same thing but is an annotation: no validator is obliged to enforce it, and the
# repository's own hermetic validator does not implement it at all. The pattern is the half a
# consumer can actually check.
BASE64_PATTERN = "^[A-Za-z0-9+/]*={0,2}$"

# proto scalar -> (JSON type, inclusive minimum, inclusive maximum). None bounds mean the JSON
# type is self-bounding (booleans, strings handled separately) or that the protobuf type does not
# constrain the value beyond what JSON already does (floating point).
SCALAR_PROJECTION: dict[str, tuple[str, int | None, int | None]] = {
    "TYPE_BOOL": ("boolean", None, None),
    "TYPE_BYTES": ("bytes", None, None),
    "TYPE_DOUBLE": ("number", None, None),
    "TYPE_FIXED32": ("integer", 0, 2**32 - 1),
    "TYPE_FIXED64": ("integer", 0, 2**64 - 1),
    "TYPE_FLOAT": ("number", None, None),
    "TYPE_INT32": ("integer", -(2**31), 2**31 - 1),
    "TYPE_INT64": ("integer", -(2**63), 2**63 - 1),
    "TYPE_SFIXED32": ("integer", -(2**31), 2**31 - 1),
    "TYPE_SFIXED64": ("integer", -(2**63), 2**63 - 1),
    "TYPE_SINT32": ("integer", -(2**31), 2**31 - 1),
    "TYPE_SINT64": ("integer", -(2**63), 2**63 - 1),
    "TYPE_STRING": ("string", None, None),
    "TYPE_UINT32": ("integer", 0, 2**32 - 1),
    "TYPE_UINT64": ("integer", 0, 2**64 - 1),
}

# Well-known types carry their own proto3 JSON encoding, so their bounds are derived here rather
# than declared per field. Anything else outside `mindclade.` fails closed: an import the
# projection has no rule for must be a decision, not a default.
WELL_KNOWN_PROJECTION: dict[str, dict[str, Any]] = {
    # RFC 3339 UTC, e.g. "2026-08-23T09:00:30.000000001Z". 64 characters is well past the
    # longest form protobuf emits.
    "google.protobuf.Timestamp": {"format": "date-time", "maxLength": 64, "type": "string"},
    # Decimal seconds with up to nine fractional digits and a trailing "s".
    "google.protobuf.Duration": {
        "maxLength": 32,
        "pattern": r"^-?(?:0|[1-9][0-9]*)(?:\.[0-9]{1,9})?s$",
        "type": "string",
    },
}

STRING_REFINEMENTS = frozenset({"maxLength", "minLength", "pattern"})
NUMERIC_REFINEMENTS = frozenset({"maximum", "minimum"})
REPEATED_REFINEMENTS = frozenset({"items", "maxItems", "minItems"})


class PolicyError(Exception):
    """The projection policy or the descriptor surface cannot produce a schema.

    Raised rather than returned because every case is a contract defect that must stop the
    generator: continuing would write a schema that does not describe the message.
    """


class Descriptor:
    """Flat FQN -> declaration index over the governed descriptor surface."""

    def __init__(self, surface: dict[str, Any]) -> None:
        version = surface.get("schema_version")
        if version != 2:
            raise PolicyError(
                f"descriptor surface schema_version is {version!r}; this generator reads 2"
            )
        packages = surface.get("packages")
        if not isinstance(packages, dict) or not packages:
            raise PolicyError("descriptor surface declares no packages")
        self.messages: dict[str, Any] = {}
        self.enums: dict[str, Any] = {}
        for package in packages.values():
            if not isinstance(package, dict):
                raise PolicyError("descriptor surface package entry is not an object")
            self.messages.update(package.get("messages") or {})
            self.enums.update(package.get("enums") or {})

    def message(self, name: str) -> dict[str, Any]:
        try:
            return self.messages[name]
        except KeyError:
            raise PolicyError(f"{name} is not a message in the descriptor surface") from None

    def fields(self, name: str) -> list[dict[str, Any]]:
        fields = self.message(name).get("fields")
        if not isinstance(fields, list):
            raise PolicyError(f"{name}: descriptor surface fields are not a list")
        return fields

    def is_map_entry(self, name: str) -> bool:
        """True for the entry message protoc synthesizes for a `map<K, V>` field.

        protoc rewrites `map<K, V> m = 1;` into `repeated MEntry m = 1;` with a nested
        `MEntry { K key = 1; V value = 2; }`. The governed descriptor surface flattens nested
        messages into the same table as declared ones and drops the `map_entry` option that
        would identify them, so shape is the only signal left: exactly two fields, numbered 1
        and 2, named `key` and `value`.

        Without this the entry message reaches the ordinary message branch and projects as an
        array of `{key, value}` objects — a schema that rejects every document proto3 JSON
        actually emits, with no error from the generator.
        """
        fields = self.messages.get(name, {}).get("fields")
        if not isinstance(fields, list) or len(fields) != 2:
            return False
        return [(str(f.get("name")), f.get("number")) for f in fields] == [("key", 1), ("value", 2)]


def _refinements(policy_message: dict[str, Any], field_name: str) -> dict[str, Any]:
    refinement = (policy_message.get("fields") or {}).get(field_name, {})
    if not isinstance(refinement, dict):
        raise PolicyError(f"refinement for {field_name!r} must be an object")
    return refinement


def _apply_refinements(
    schema: dict[str, Any],
    refinement: dict[str, Any],
    *,
    allowed: frozenset[str],
    where: str,
) -> dict[str, Any]:
    """Merge policy refinements onto a derived schema, rejecting anything that widens it."""

    unknown = sorted(set(refinement) - allowed)
    if unknown:
        raise PolicyError(
            f"{where}: refinement(s) {unknown} are not valid for this projection; "
            f"allowed here: {sorted(allowed)}"
        )
    merged = dict(schema)
    for keyword, value in refinement.items():
        if keyword == "items":
            continue
        # isinstance(True, int) is true in Python, so the bool exclusion is load-bearing:
        # without it `{"minimum": true}` passes every guard below and renders literally into a
        # checked-in schema, where `true` is not a numeric bound.
        if keyword in {"maximum", "minimum"} and (
            not isinstance(value, int) or isinstance(value, bool)
        ):
            raise PolicyError(f"{where}.{keyword}: must be an integer")
        if keyword in {"maxItems", "maxLength", "minItems", "minLength"} and (
            not isinstance(value, int) or isinstance(value, bool) or value < 0
        ):
            raise PolicyError(f"{where}.{keyword}: must be a non-negative integer")
        if keyword == "pattern" and not isinstance(value, str):
            raise PolicyError(f"{where}.pattern: must be a string")
        merged[keyword] = value

    derived_minimum = schema.get("minimum")
    derived_maximum = schema.get("maximum")
    if (
        isinstance(derived_minimum, int)
        and merged.get("minimum", derived_minimum) < derived_minimum
    ):
        raise PolicyError(
            f"{where}.minimum: {merged['minimum']} widens the protobuf range "
            f"(descriptor minimum is {derived_minimum})"
        )
    if (
        isinstance(derived_maximum, int)
        and merged.get("maximum", derived_maximum) > derived_maximum
    ):
        raise PolicyError(
            f"{where}.maximum: {merged['maximum']} widens the protobuf range "
            f"(descriptor maximum is {derived_maximum})"
        )
    minimum = merged.get("minimum")
    maximum = merged.get("maximum")
    if isinstance(minimum, int) and isinstance(maximum, int) and minimum > maximum:
        raise PolicyError(f"{where}: minimum {minimum} exceeds maximum {maximum}")
    min_length = merged.get("minLength")
    max_length = merged.get("maxLength")
    if isinstance(min_length, int) and isinstance(max_length, int) and min_length > max_length:
        raise PolicyError(f"{where}: minLength {min_length} exceeds maxLength {max_length}")
    min_items = merged.get("minItems")
    max_items = merged.get("maxItems")
    if isinstance(min_items, int) and isinstance(max_items, int) and min_items > max_items:
        raise PolicyError(f"{where}: minItems {min_items} exceeds maxItems {max_items}")
    return merged


def _require_bounds(schema: dict[str, Any], where: str) -> None:
    """Reject any projection a consumer would have to read without a limit."""

    if "$ref" in schema:
        return
    kind = schema.get("type")
    if kind == "array":
        if "maxItems" not in schema:
            raise PolicyError(f"{where}: repeated field needs a maxItems bound")
        items = schema.get("items")
        if isinstance(items, dict):
            _require_bounds(items, f"{where}.items")
        return
    if kind == "string":
        if "enum" in schema:
            return
        if "maxLength" not in schema:
            raise PolicyError(f"{where}: string projection needs a maxLength bound")
        return
    if kind == "integer":
        minimum = schema.get("minimum")
        maximum = schema.get("maximum")
        if not isinstance(minimum, int) or not isinstance(maximum, int):
            raise PolicyError(f"{where}: integer projection needs minimum and maximum")
        if minimum < -SAFE_JSON_INTEGER or maximum > SAFE_JSON_INTEGER:
            raise PolicyError(
                f"{where}: [{minimum}, {maximum}] leaves the range a JSON number represents "
                f"exactly (+/-{SAFE_JSON_INTEGER}); declare a tighter bound in "
                f"{POLICY_RELPATH} or project the field as a string"
            )


def _element_schema(
    descriptor: Descriptor,
    field: dict[str, Any],
    refinement: dict[str, Any],
    *,
    where: str,
    pending: list[str],
) -> dict[str, Any]:
    """Project one non-repeated protobuf value, ignoring cardinality."""

    proto_type = field.get("type")
    if not isinstance(proto_type, str):
        raise PolicyError(f"{where}: descriptor field has no type")

    if proto_type in SCALAR_PROJECTION:
        json_type, minimum, maximum = SCALAR_PROJECTION[proto_type]
        if json_type == "bytes":
            base: dict[str, Any] = {
                "contentEncoding": "base64",
                "pattern": BASE64_PATTERN,
                "type": "string",
            }
            return _apply_refinements(base, refinement, allowed=STRING_REFINEMENTS, where=where)
        if json_type == "string":
            return _apply_refinements(
                {"type": "string"}, refinement, allowed=STRING_REFINEMENTS, where=where
            )
        if json_type == "integer":
            base = {"type": "integer"}
            if minimum is not None:
                base["minimum"] = minimum
            if maximum is not None:
                base["maximum"] = maximum
            return _apply_refinements(base, refinement, allowed=NUMERIC_REFINEMENTS, where=where)
        if json_type == "number":
            return _apply_refinements(
                {"type": "number"}, refinement, allowed=NUMERIC_REFINEMENTS, where=where
            )
        return _apply_refinements({"type": "boolean"}, refinement, allowed=frozenset(), where=where)

    if proto_type in WELL_KNOWN_PROJECTION:
        base = dict(WELL_KNOWN_PROJECTION[proto_type])
        return _apply_refinements(base, refinement, allowed=STRING_REFINEMENTS, where=where)

    if proto_type in descriptor.enums:
        values = descriptor.enums[proto_type].get("values") or []
        names = [value["name"] for value in values]
        if not names:
            raise PolicyError(f"{where}: enum {proto_type} declares no values")
        # proto3 JSON writes enum names. Accepting the numeric form too would let a producer
        # emit a value this closed list has never reviewed.
        return _apply_refinements(
            {"enum": names, "type": "string"}, refinement, allowed=frozenset(), where=where
        )

    # Checked before the message branch, which would otherwise accept the synthesized entry
    # message and emit an array of {key, value} pairs. proto3 JSON encodes a map as an object
    # keyed by the map key, and the reduced descriptor surface carries neither the map_entry
    # option nor a key type to derive `propertyNames` from — so the correct projection is not
    # derivable here and a plausible wrong one is worse than a refusal.
    if field.get("label") == "LABEL_REPEATED" and descriptor.is_map_entry(proto_type):
        raise PolicyError(
            f"{where}: {proto_type} is a protobuf map entry, and map fields are not projected. "
            "proto3 JSON writes a map as an object keyed by the map key; the governed "
            "descriptor surface drops the map_entry option and carries no key type, so that "
            "object cannot be derived here. Projecting it as an array of {key, value} pairs "
            "would be a schema no producer emits, so this fails closed instead."
        )

    if proto_type in descriptor.messages:
        if refinement:
            raise PolicyError(
                f"{where}: refine {proto_type} under messages[{proto_type!r}], not at the "
                "referencing field"
            )
        if proto_type not in pending:
            pending.append(proto_type)
        return {"$ref": f"#/$defs/{proto_type}"}

    raise PolicyError(
        f"{where}: no projection rule for protobuf type {proto_type!r}. Groups and unmapped "
        "imports are deliberately unsupported: the governed descriptor surface does not carry "
        "enough information to project them correctly."
    )


def _project_message(
    descriptor: Descriptor,
    policy: dict[str, Any],
    name: str,
    *,
    pending: list[str],
    consumed: set[str],
) -> dict[str, Any]:
    """Project one protobuf message into a closed JSON Schema object.

    `consumed` collects every policy `messages` entry a projection reaches, so build() can
    reject entries that no projection uses. The field-orphan check below only runs for messages
    something reaches; an unreachable entry would otherwise be validated against nothing.
    """

    consumed.add(name)
    messages = policy.get("messages")
    if not isinstance(messages, dict) or name not in messages:
        raise PolicyError(f"{name}: no entry under messages in {POLICY_RELPATH}")
    entry = messages[name]
    if not isinstance(entry, dict):
        raise PolicyError(f"messages[{name!r}] must be an object")

    fields = descriptor.fields(name)
    by_name = {str(field["name"]): field for field in fields}
    declared = set(entry.get("fields") or {})
    orphans = sorted(declared - set(by_name))
    if orphans:
        raise PolicyError(
            f"{name}: policy refines field(s) {orphans} that the descriptor no longer declares; "
            "the protobuf message changed and this policy did not"
        )

    properties: dict[str, Any] = {}
    for field in fields:
        field_name = str(field["name"])
        where = f"{name}.{field_name}"
        refinement = _refinements(entry, field_name)
        if field.get("label") == "LABEL_REPEATED":
            unknown = sorted(set(refinement) - REPEATED_REFINEMENTS)
            if unknown:
                raise PolicyError(
                    f"{where}: refinement(s) {unknown} are not valid for a repeated field; "
                    f"allowed here: {sorted(REPEATED_REFINEMENTS)}"
                )
            item_refinement = refinement.get("items") or {}
            if not isinstance(item_refinement, dict):
                raise PolicyError(f"{where}.items: must be an object")
            items = _element_schema(
                descriptor, field, item_refinement, where=f"{where}.items", pending=pending
            )
            # Routed through _apply_refinements rather than merged inline, so array bounds get
            # the same type and min-versus-max checks every other bound gets. Setting them by
            # hand is how `{"maxItems": 2, "minItems": 9}` used to render unchallenged.
            projected = _apply_refinements(
                {"items": items, "type": "array"},
                {key: value for key, value in refinement.items() if key != "items"},
                allowed=REPEATED_REFINEMENTS - {"items"},
                where=where,
            )
        else:
            projected = _element_schema(descriptor, field, refinement, where=where, pending=pending)
        _require_bounds(projected, where)
        properties[field_name] = projected

    required = entry.get("required", [])
    if not isinstance(required, list) or not all(isinstance(item, str) for item in required):
        raise PolicyError(f"{name}.required: must be a list of field names")
    if len(set(required)) != len(required):
        raise PolicyError(f"{name}.required: entries must be unique")
    missing = sorted(set(required) - set(by_name))
    if missing:
        raise PolicyError(
            f"{name}: required field(s) {missing} are not declared by the descriptor; "
            "the protobuf message changed and this policy did not"
        )
    in_oneof = sorted(item for item in required if by_name[item].get("oneof"))
    if in_oneof:
        raise PolicyError(
            f"{name}: field(s) {in_oneof} belong to a oneof and cannot be required; at most one "
            "member of a oneof is ever present"
        )

    schema: dict[str, Any] = {
        "additionalProperties": False,
        "properties": properties,
        "type": "object",
    }
    if required:
        # Field-number order, so the wire order a reader sees in the .proto is the order the
        # schema lists. Policy order is not trusted for this: it would make the output depend on
        # how someone happened to type the list.
        order = {str(field["name"]): int(field["number"]) for field in fields}
        schema["required"] = sorted(required, key=lambda item: order[item])
    title = entry.get("title")
    if title is not None:
        if not isinstance(title, str) or not title:
            raise PolicyError(f"{name}.title: must be a non-empty string")
        schema["title"] = title
    return schema


def _definitions(
    descriptor: Descriptor,
    policy: dict[str, Any],
    pending: list[str],
    consumed: set[str],
) -> dict[str, Any]:
    """Project every message reachable from the roots into a `$defs` block."""

    definitions: dict[str, Any] = {}
    while pending:
        name = pending.pop(0)
        if name in definitions:
            continue
        # Reserve the slot before recursing so a self-referencing message terminates.
        definitions[name] = {}
        definitions[name] = _project_message(
            descriptor, policy, name, pending=pending, consumed=consumed
        )
    return definitions


def _identifier(policy: dict[str, Any], path: str) -> str:
    prefix = policy.get("id_prefix")
    if not isinstance(prefix, str) or not prefix.endswith("/"):
        raise PolicyError("id_prefix must be a string ending in '/'")
    return prefix + path.removesuffix(".schema.json") + ".json"


def _message_projection(
    descriptor: Descriptor,
    policy: dict[str, Any],
    projection: dict[str, Any],
    consumed: set[str],
) -> dict[str, Any]:
    name = projection.get("message")
    if not isinstance(name, str):
        raise PolicyError(f"{projection.get('path')!r}: message projection needs a message name")
    pending: list[str] = []
    schema = _project_message(descriptor, policy, name, pending=pending, consumed=consumed)
    definitions = _definitions(descriptor, policy, pending, consumed)
    schema["$id"] = _identifier(policy, str(projection["path"]))
    schema["$schema"] = JSON_SCHEMA_DIALECT
    if definitions:
        schema["$defs"] = definitions
    return schema


def _payload_union_projection(
    descriptor: Descriptor,
    policy: dict[str, Any],
    projection: dict[str, Any],
    consumed: set[str],
) -> dict[str, Any]:
    path = str(projection["path"])
    title = projection.get("title")
    if not isinstance(title, str) or not title:
        raise PolicyError(f"{path}: payload_union projection needs a title")
    names = projection.get("payload_messages")
    if not isinstance(names, list) or not all(isinstance(item, str) for item in names):
        raise PolicyError(f"{path}: payload_messages must be a list of message names")
    if len(set(names)) != len(names):
        raise PolicyError(f"{path}: payload_messages entries must be unique")

    schema: dict[str, Any] = {
        "$id": _identifier(policy, path),
        "$schema": JSON_SCHEMA_DIALECT,
        "title": title,
    }
    if not names:
        # `{}` — what this file used to hold — accepts every document, including one whose
        # every field is wrong. An undeclared payload contract accepts nothing instead, so a
        # consumer that reaches for it fails loudly rather than being told anything is valid.
        schema["description"] = (
            "No protobuf payload message is declared for this domain in "
            f"{POLICY_RELPATH}, so this schema accepts no document. Declare the domain's event "
            "payload messages there to widen it; do not edit this file."
        )
        schema["not"] = {}
        return schema

    pending = list(names)
    schema["$defs"] = _definitions(descriptor, policy, pending, consumed)
    # anyOf, not oneOf. Every proto3 field is optional, so payload schemas overlap by
    # construction: a document valid for a message carrying {reason} is also valid for one
    # carrying {reason, note}. oneOf demands EXACTLY one branch match, so it would reject the
    # very documents both branches accept.
    schema["anyOf"] = [{"$ref": f"#/$defs/{name}"} for name in names]
    return schema


def render(schema: dict[str, Any]) -> str:
    """Serialize one schema in the repository's checked-in JSON contract format."""

    return json.dumps(schema, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def build(root: Path, policy: dict[str, Any], surface: dict[str, Any]) -> dict[Path, str]:
    """Return the complete generated tree as {absolute path: file content}."""

    if policy.get("schema_version") != 1:
        raise PolicyError(
            f"{POLICY_RELPATH}: schema_version is {policy.get('schema_version')!r}; expected 1"
        )
    output_root_relpath = policy.get("output_root")
    if not isinstance(output_root_relpath, str) or not output_root_relpath:
        raise PolicyError(f"{POLICY_RELPATH}: output_root must be a non-empty path")
    output_root = (root / output_root_relpath).resolve()
    descriptor = Descriptor(surface)

    projections = policy.get("projections")
    if not isinstance(projections, list) or not projections:
        raise PolicyError(f"{POLICY_RELPATH}: projections must be a non-empty list")

    declared = policy.get("messages")
    if declared is not None and not isinstance(declared, dict):
        raise PolicyError(f"{POLICY_RELPATH}: messages must be an object")
    declared_names = set(declared or {})
    absent = sorted(declared_names - set(descriptor.messages))
    if absent:
        raise PolicyError(
            f"{POLICY_RELPATH}: messages entr(ies) {absent} name protobuf messages the "
            "descriptor surface does not declare; the .proto changed and this policy did not"
        )

    rendered: dict[Path, str] = {}
    identifiers: dict[str, str] = {}
    consumed: set[str] = set()
    for projection in projections:
        if not isinstance(projection, dict):
            raise PolicyError(f"{POLICY_RELPATH}: each projection must be an object")
        path = projection.get("path")
        if not isinstance(path, str) or not path.endswith(".schema.json"):
            raise PolicyError(f"{POLICY_RELPATH}: projection path {path!r} must end .schema.json")
        target = (output_root / path).resolve()
        if not target.is_relative_to(output_root):
            raise PolicyError(f"{path}: projection escapes {output_root_relpath}")
        if target in rendered:
            raise PolicyError(f"{path}: two projections write the same file")
        kind = projection.get("kind")
        if kind == "message":
            schema = _message_projection(descriptor, policy, projection, consumed)
        elif kind == "payload_union":
            schema = _payload_union_projection(descriptor, policy, projection, consumed)
        else:
            raise PolicyError(f"{path}: unknown projection kind {kind!r}")
        identifier = str(schema["$id"])
        if identifier in identifiers:
            # Distinct documents sharing one $id is the defect this tree shipped with: eight
            # copies of the envelope all claimed .../events/v1/envelope.json, so any registry
            # loading more than one of them silently kept whichever it saw last.
            raise PolicyError(
                f"{path}: $id {identifier} is already claimed by {identifiers[identifier]}"
            )
        identifiers[identifier] = path
        rendered[target] = render(schema)

    # An entry no projection reaches is never checked against the descriptor's field set, so its
    # refinements can name fields that no longer exist and nothing notices. Requiring every entry
    # to be reachable is what keeps "the policy cannot outlive the proto" true for the whole file
    # rather than for the part that happens to be wired up.
    unused = sorted(declared_names - consumed)
    if unused:
        raise PolicyError(
            f"{POLICY_RELPATH}: messages entr(ies) {unused} are never reached by a projection, "
            "so their refinements are validated against nothing; project them or delete them"
        )
    return rendered


def _orphans(root: Path, policy: dict[str, Any], expected: set[Path]) -> list[str]:
    """Schema files under the generated tree that no projection produces."""

    output_root = (root / str(policy["output_root"])).resolve()
    if not output_root.is_dir():
        return []
    found = {path.resolve() for path in output_root.rglob("*.schema.json")}
    return sorted(path.relative_to(root).as_posix() for path in found - expected)


def generate(root: Path, *, check: bool) -> tuple[int, list[str]]:
    """Write or verify the generated tree. Returns (exit code, messages)."""

    policy_path = root / POLICY_RELPATH
    try:
        policy = load_json_yaml(policy_path)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return 1, [f"{POLICY_RELPATH}: {exc}"]

    surface_relpath = policy.get("descriptor_surface")
    if not isinstance(surface_relpath, str) or not surface_relpath:
        return 1, [f"{POLICY_RELPATH}: descriptor_surface must be a non-empty path"]
    surface_path = root / surface_relpath
    try:
        surface = json.loads(surface_path.read_text(encoding="utf-8"))
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return 1, [f"{surface_relpath}: {exc}"]
    if not isinstance(surface, dict):
        return 1, [f"{surface_relpath}: descriptor surface must be an object"]

    try:
        rendered = build(root, policy, surface)
    except PolicyError as exc:
        return 1, [str(exc)]

    changed: list[str] = []
    for path, content in sorted(rendered.items()):
        relative = path.relative_to(root).as_posix()
        current = path.read_text(encoding="utf-8") if path.is_file() else None
        if current == content:
            continue
        if check:
            changed.append(f"{relative}: {'generated drift' if current else 'missing'}")
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        changed.append(f"wrote {relative}")

    # Computed AFTER the writes. Running it first meant that renaming a projection path made
    # the generator refuse to write anything at all, so `generate_jsonschema.py` could not
    # converge until the stale file was deleted by hand — the one invocation that is supposed to
    # fix things did nothing.
    problems = []
    if orphans := _orphans(root, policy, set(rendered)):
        problems.append(
            "schema file(s) under the generated tree that no projection produces: "
            + ", ".join(orphans)
            + f"; add a projection to {POLICY_RELPATH} or delete them"
        )

    total = len(rendered)
    if check:
        if changed:
            problems.append(
                "run `python3 tools/codegen/generate_jsonschema.py` and commit the result"
            )
        if changed or problems:
            return 1, [*changed, *problems]
        return 0, [f"event JSON Schemas are current across {total} generated file(s)"]
    if problems:
        return 1, [*changed, *problems]
    return 0, changed or [f"event JSON Schemas were already current across {total} file(s)"]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    # BUILD_WORKSPACE_DIRECTORY, so `bazel run` writes into the source tree rather than into the
    # runfiles copy it would otherwise resolve from __file__ and then discard.
    default_root = Path(os.environ.get("BUILD_WORKSPACE_DIRECTORY", ROOT))
    parser.add_argument("--repo", type=Path, default=default_root, help="repository root")
    parser.add_argument(
        "--check",
        action="store_true",
        help="fail when a generated schema differs from the protobuf projection",
    )
    args = parser.parse_args(argv)
    code, messages = generate(args.repo.resolve(), check=args.check)
    stream = sys.stderr if code else sys.stdout
    for message in messages:
        print(message, file=stream)
    return code


if __name__ == "__main__":
    raise SystemExit(main())
