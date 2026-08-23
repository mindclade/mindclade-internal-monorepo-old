# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Conformance between the event-envelope proto and its published JSON schemas.

Durable events cross languages twice: producers and consumers that speak
protobuf use `mindclade.events.v1.EventEnvelope`, and everything on the JSON
side — the broker contracts, the AsyncAPI projection, and any non-protobuf
consumer — validates against `protocols/events/generated/<domain>/v1/
event-envelope.schema.json`. There are eight copies of that schema, one per
event domain, and no generator step or gate compares any of them with the proto.

The previous version of this module read one of the eight, asserted that a
subset of its `required` list was present, and grepped the proto text for
"message EventEnvelope" and "payload_digest". A field added to the proto and
not to the schema, a field dropped from the schema, a `required` entry lost, a
type changed from integer to string, or seven of the eight copies drifting away
from the eighth all pass that. This module compares them field for field.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest
from test_worker_protocol import PROTO_ROOT, proto_messages

ROOT = Path(__file__).resolve().parents[3]
EVENTS_V1 = PROTO_ROOT / "events/v1"
GENERATED = ROOT / "protocols/events/generated"
SCHEMA_NAME = "event-envelope.schema.json"


def event_domains() -> list[str]:
    """Every published event domain, derived from the tree rather than counted.

    A floor like `len(paths) >= 8` passes when a ninth domain ships without an
    envelope schema, which is the case the comparison below exists to catch.
    """
    return sorted(path.parent.name for path in GENERATED.glob("*/v1"))


@pytest.fixture(scope="module")
def envelope_schema() -> dict:
    return json.loads((GENERATED / "runtime/v1" / SCHEMA_NAME).read_text())


@pytest.fixture(scope="module")
def envelope_proto() -> dict[str, dict[str, object]]:
    return proto_messages(EVENTS_V1)["EventEnvelope"]


# Fields a producer may legitimately omit. Everything else is required, and this
# split is the reviewed contract: `workspace_id` because tenant-scoped events
# have no workspace, and the two tracing identifiers because a root event has no
# cause. A field moving across this line changes what a consumer may assume is
# present, so it fails here rather than at 3am in a replay.
OPTIONAL_FIELDS = {"workspace_id", "correlation_id", "causation_id"}

# proto declared type -> the JSON Schema `type` that must carry it.
JSON_TYPE_FOR = {"string": "string", "uint64": "integer", "bytes": "string"}


def _schema_canonical(path: Path) -> bytes:
    """Return a canonical serialization of the schema with ``$id`` excluded.

    Each domain copy carries a distinct ``$id`` (e.g.
    ``https://schemas.mindclade.dev/events/runtime/v1/event-envelope.json``)
    so that schema tooling can resolve it in a self-contained bundle.  That
    field is intentionally per-domain and must not be treated as drift when
    comparing whether all copies carry the same contract.
    """
    schema = json.loads(path.read_bytes())
    schema.pop("$id", None)
    return json.dumps(schema, sort_keys=True, indent=2).encode()


def test_every_domain_publishes_the_identical_envelope_schema():
    """One envelope contract, eight published copies; they must not diverge.

    Each domain gets its own file so its generated bundle is self-contained. A
    consumer that validates against the artifact schema and a producer that
    emits against the training schema have to agree, so the copies are compared
    field for field (excluding the domain-specific ``$id``) rather than merely
    being present.
    """
    domains = event_domains()
    assert domains, f"no event domains under {GENERATED}"
    missing = [name for name in domains if not (GENERATED / name / "v1" / SCHEMA_NAME).is_file()]
    assert not missing, f"event domains published with no envelope schema: {missing}"
    reference_path = GENERATED / domains[0] / "v1" / SCHEMA_NAME
    reference = _schema_canonical(reference_path)
    for name in domains[1:]:
        path = GENERATED / name / "v1" / SCHEMA_NAME
        assert _schema_canonical(path) == reference, (
            f"{path.relative_to(ROOT)} has drifted from {reference_path.relative_to(ROOT)}"
        )


def test_schema_properties_match_the_proto_field_set(envelope_schema, envelope_proto):
    assert set(envelope_schema["properties"]) == set(envelope_proto), (
        "the envelope proto and JSON schema disagree on fields: "
        f"{sorted(set(envelope_schema['properties']) ^ set(envelope_proto))}"
    )


def test_required_is_every_field_that_is_not_declared_optional(envelope_schema):
    properties = set(envelope_schema["properties"])
    assert properties > OPTIONAL_FIELDS, "an optional field name no longer exists"
    assert set(envelope_schema["required"]) == properties - OPTIONAL_FIELDS, (
        "required fields drifted: "
        f"{sorted(set(envelope_schema['required']) ^ (properties - OPTIONAL_FIELDS))}"
    )


def test_schema_types_match_the_declared_wire_types(envelope_schema, envelope_proto):
    """A uint64 that becomes a JSON string, or the reverse, silently breaks parsers."""
    for field, spec in envelope_proto.items():
        assert spec["label"] == "singular", f"{field} is repeated on the wire but scalar in JSON"
        declared = envelope_schema["properties"][field]
        assert declared["type"] == JSON_TYPE_FOR[str(spec["type"])], (
            f"{field} is {spec['type']} on the wire but {declared['type']} in the schema"
        )
        if spec["type"] == "uint64":
            # proto3 uint64 is unsigned; JSON Schema has no unsigned integer, so
            # the lower bound is what stops a negative value being accepted here
            # and then failing to encode on the protobuf side.
            assert declared.get("minimum", -1) >= 0, f"{field} accepts negative integers"
        if spec["type"] == "bytes":
            assert declared.get("contentEncoding") == "base64", (
                f"{field} carries bytes but declares no transfer encoding"
            )


# Ratchet, not an allowlist. Every string in the envelope must carry either a
# maxLength or a length-fixing anchored pattern. The set is asserted exactly so
# this can only shrink: a new unbounded field fails, and removing an entry once
# the schema is fixed is required.
UNBOUNDED_ENVELOPE_STRINGS: set[str] = set()

# `*`, `+`, and `{n,}` are open-ended, so `^workspace_.*$` is anchored and still
# accepts a gigabyte. Only a pattern with no unbounded quantifier fixes a length.
_OPEN_ENDED = re.compile(r"[*+]|\{\d+,\}")


def _is_length_bounded(declared: dict) -> bool:
    if "maxLength" in declared:
        return True
    pattern = declared.get("pattern", "")
    return bool(pattern) and not _OPEN_ENDED.search(pattern)


def test_envelope_is_closed_and_string_bounds_only_ratchet_tighter(envelope_schema):
    """A durable envelope is parsed from an untrusted broker payload.

    Every string needs an explicit length ceiling or a pattern that fixes its
    length, or one oversized field turns into unbounded memory in every consumer
    that decodes the event before deciding whether it wants it.
    """
    assert envelope_schema["additionalProperties"] is False
    unbounded = {
        field
        for field, declared in envelope_schema["properties"].items()
        if declared["type"] == "string" and not _is_length_bounded(declared)
    }
    assert unbounded == UNBOUNDED_ENVELOPE_STRINGS, (
        "envelope string bounds changed; a new unbounded field is a defect and a "
        f"fixed one must leave the ratchet: {sorted(unbounded ^ UNBOUNDED_ENVELOPE_STRINGS)}"
    )


def test_payload_bound_is_a_real_byte_ceiling(envelope_schema):
    """`maxLength` counts base64 characters, so it has to be derived from bytes.

    A limit written directly in characters reads as a byte budget and is not one.
    The declared value must be exactly the base64 expansion of the intended
    payload ceiling, padding included.
    """
    limit = envelope_schema["properties"]["payload"]["maxLength"]
    payload_bytes = 1 << 20
    expected = -(-payload_bytes // 3) * 4
    assert limit == expected, (
        f"payload maxLength is {limit} base64 characters; a {payload_bytes}-byte ceiling "
        f"encodes to {expected}. Larger scientific data is referenced through artifacts."
    )


def test_identity_fields_are_pattern_anchored(envelope_schema):
    """Identity comes from the value's shape, not from the producer's word for it."""
    for field, prefix in (
        ("event_id", "evt_"),
        ("tenant_id", "tenant_"),
        ("payload_digest", "sha256:"),
    ):
        pattern = envelope_schema["properties"][field].get("pattern", "")
        assert pattern.startswith(f"^{prefix}") and pattern.endswith("$"), (
            f"{field} pattern {pattern!r} is not anchored to the {prefix!r} form"
        )


def test_envelope_is_versioned_bounded_and_replayable(envelope_schema, envelope_proto):
    """At-least-once delivery is only safe if a replay is detectable.

    `aggregate_id` plus `aggregate_version` identify the event, `ordering_key`
    fixes the partition it replays within, and `payload_digest` lets a consumer
    prove the bytes it re-reads are the bytes that were committed.
    """
    for field in ("aggregate_id", "aggregate_version", "ordering_key", "payload_digest"):
        assert field in envelope_proto, f"{field} left the wire contract"
        assert field in envelope_schema["required"], f"{field} is no longer required"
    assert envelope_schema["properties"]["aggregate_version"]["minimum"] == 1, (
        "aggregate_version 0 is indistinguishable from an unset proto3 field"
    )


def test_time_is_unix_milliseconds_and_monotonic_in_the_schema(envelope_schema, envelope_proto):
    """One time unit, named in the field, with no zero value that means 'unset'."""
    times = [field for field in envelope_proto if "_at_" in field or field.endswith("_millis")]
    assert times == ["occurred_at_unix_millis"], f"unexpected time fields: {times}"
    for field in envelope_proto:
        assert not field.endswith(("_seconds", "_micros", "_nanos", "_at")), (
            f"{field} uses a time unit other than the contract's milliseconds"
        )
    assert envelope_schema["properties"]["occurred_at_unix_millis"]["minimum"] == 1


def test_the_schema_declares_the_mindclade_event_authority(envelope_schema):
    assert envelope_schema["$schema"] == "https://json-schema.org/draft/2020-12/schema"
    assert envelope_schema["$id"].startswith("https://schemas.mindclade.dev/events/")
    assert envelope_schema["type"] == "object"
