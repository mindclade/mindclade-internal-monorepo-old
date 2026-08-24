# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The generated event schemas are derived from the protobuf descriptor, and stay derived.

Before tools/codegen/generate_jsonschema.py existed, protocols/events/generated/ held eight
hand-maintained copies of the event envelope schema with no generator behind them and nothing
comparing any of them to `mindclade.events.v1.EventEnvelope`. A field added to the proto and not
the schema, a field dropped, a lost `required` entry, or seven copies drifting from the eighth
all passed.

Three kinds of assertion live here, and they are deliberately not the same assertion:

  drift        the checked-in tree equals what the generator produces. Catches a hand edit.
  derivation   each schema's property set equals the descriptor's field set, read straight from
               protocols/compatibility/protobuf-v1-descriptor.json rather than through the
               generator. A generator bug that dropped a field would satisfy the drift test and
               fail this one.
  fail-closed  the gates actually bite. Each mutates a copy of the descriptor or the policy and
               asserts a specific refusal, because a gate nobody has watched refuse anything is
               a gate nobody knows is wired up.

This module lives under tools/codegen/tests/ rather than beside the schemas because that is
where the repository's pytest lane looks: `testpaths` in pyproject.toml covers `tools/` and does
not cover `protocols/`, so a real-tree assertion placed next to the schemas would only ever run
when someone named the path by hand.

It has no Bazel target, for the same reason the blueprint and scaffold ratchets under
tests/integration/ have none: it reads the real repository tree. The governed descriptor surface
it needs is a source of the `//protocols` package, whose default visibility is private, so a
sandboxed target cannot reach it without widening that visibility.
"""

from __future__ import annotations

import copy
import importlib.util
import json
import re
import sys
from pathlib import Path
from typing import Any

import pytest

ROOT = Path(__file__).resolve().parents[3]
GENERATED = ROOT / "protocols/events/generated"
ENVELOPE_MESSAGE = "mindclade.events.v1.EventEnvelope"

# The producer of the orchestration domain's event payloads, and the fixtures recording what it
# writes. Named here rather than inside the one test that reads them because both halves of the
# claim below — "the schema accepts these documents" and "these documents are what the store
# emits" — have to point at the same two places.
ORCHESTRATION_STORE_EVENTS = (
    ROOT / "services/control_plane/internal/store/postgres/orchestration/events.go"
)
ORCHESTRATION_FIXTURES = ROOT / "protocols/events/fixtures/orchestration/v1"

# fixture file -> the Go struct in ORCHESTRATION_STORE_EVENTS whose marshalled form it records.
# Two structs appear twice on purpose: each has `omitempty` fields, so one fixture alone would
# only ever pin one of the two documents the store can write.
ORCHESTRATION_FIXTURE_SOURCES = {
    "attempt-failed-event.valid.json": "attemptEvent",
    "attempt-state-event.valid.json": "attemptEvent",
    "cancellation-requested-event.valid.json": "cancellationEvent",
    "cancellation-stage-scoped-event.valid.json": "cancellationEvent",
    "stage-state-event.valid.json": "stageEvent",
    "workflow-published-event.valid.json": "workflowEvent",
}


def _generator():
    """Load the generator by path.

    The codegen tools are scripts rather than an importable package, so loading by location is
    how the repository already pulls a checker into a test (see
    tests/integration/test_blueprint_scaffold.py).
    """
    path = ROOT / "tools/codegen/generate_jsonschema.py"
    spec = importlib.util.spec_from_file_location("generate_jsonschema", path)
    if spec is None or spec.loader is None:  # pragma: no cover - a broken path fails loudly
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    # Registered before exec_module: anything the module defines that later resolves its own
    # __module__ through sys.modules — a dataclass, an enum — gets None otherwise, and fails
    # with an error that names dataclasses rather than this loader.
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


GENERATOR = _generator()


def _policy() -> dict[str, Any]:
    from protocols.events.validate_contracts import load_json_yaml

    return load_json_yaml(ROOT / GENERATOR.POLICY_RELPATH)


def _surface() -> dict[str, Any]:
    return json.loads((ROOT / _policy()["descriptor_surface"]).read_text(encoding="utf-8"))


def _descriptor_fields(surface: dict[str, Any], message: str) -> list[dict[str, Any]]:
    for package in surface["packages"].values():
        if message in package["messages"]:
            return package["messages"][message]["fields"]
    raise AssertionError(f"{message} is not in the descriptor surface")


def _envelope_paths() -> list[Path]:
    return sorted(GENERATED.rglob("event-envelope.schema.json"))


def _bounds_for(field: dict[str, Any]) -> dict[str, Any]:
    """The smallest refinement that satisfies the generator's mandatory bounds.

    Used only by the projection-shape test, which asserts what a field projects TO. The bound
    rules themselves are asserted separately against the real envelope policy, so this helper
    mirroring them cannot make those assertions vacuous.
    """
    if field["label"] == "LABEL_REPEATED":
        inner = _bounds_for({**field, "label": "LABEL_OPTIONAL"})
        return {"maxItems": 64, **({"items": inner} if inner else {})}
    if field["type"] in {"TYPE_STRING", "TYPE_BYTES"}:
        return {"maxLength": 256}
    if field["type"] in {
        "TYPE_INT64",
        "TYPE_UINT64",
        "TYPE_FIXED64",
        "TYPE_SFIXED64",
        "TYPE_SINT64",
    }:
        return {"maximum": 2**53 - 1, "minimum": 0}
    return {}


def _minimal_policy(
    surface: dict[str, Any], roots: list[str], *, projections: list[dict[str, Any]] | None = None
) -> dict[str, Any]:
    """A policy projecting `roots` and everything they reference, with bounds satisfied."""

    known = {name for package in surface["packages"].values() for name in package["messages"]}
    messages: dict[str, Any] = {}
    pending = list(roots)
    while pending:
        name = pending.pop(0)
        if name in messages:
            continue
        fields = _descriptor_fields(surface, name)
        messages[name] = {
            "fields": {field["name"]: bounds for field in fields if (bounds := _bounds_for(field))}
        }
        pending.extend(field["type"] for field in fields if field["type"] in known)
    if projections is None:
        projections = [
            {
                "kind": "message",
                "message": roots[0],
                "path": "runtime/v1/event-envelope.schema.json",
            }
        ]
    return {
        "schema_version": 1,
        "descriptor_surface": "protocols/compatibility/protobuf-v1-descriptor.json",
        "output_root": "protocols/events/generated",
        "id_prefix": "https://schemas.mindclade.dev/events/",
        "messages": messages,
        "projections": projections,
    }


# ---------------------------------------------------------------------------------------
# drift
# ---------------------------------------------------------------------------------------


def test_generated_tree_matches_the_protobuf_projection() -> None:
    code, messages = GENERATOR.generate(ROOT, check=True)
    assert code == 0, "\n".join(messages)


def test_generation_is_idempotent() -> None:
    """Two runs must produce the same bytes, or --check could never be trusted."""

    policy = _policy()
    surface = _surface()
    first = GENERATOR.build(ROOT, policy, surface)
    second = GENERATOR.build(ROOT, policy, surface)
    assert first == second
    for path, content in first.items():
        assert path.read_text(encoding="utf-8") == content, path


# ---------------------------------------------------------------------------------------
# derivation
# ---------------------------------------------------------------------------------------


def test_every_envelope_copy_carries_every_protobuf_field() -> None:
    """Checked against the descriptor directly, not against the generator's own output."""

    expected = {field["name"] for field in _descriptor_fields(_surface(), ENVELOPE_MESSAGE)}
    paths = _envelope_paths()
    assert len(paths) == 8, "the blueprint reserves one envelope schema per event domain"
    for path in paths:
        schema = json.loads(path.read_text(encoding="utf-8"))
        assert set(schema["properties"]) == expected, path
        assert schema["additionalProperties"] is False, path


def test_envelope_required_fields_are_listed_in_field_number_order() -> None:
    fields = _descriptor_fields(_surface(), ENVELOPE_MESSAGE)
    order = {field["name"]: field["number"] for field in fields}
    for path in _envelope_paths():
        required = json.loads(path.read_text(encoding="utf-8"))["required"]
        assert required == sorted(required, key=lambda name: order[name]), path


def test_envelope_copies_differ_only_by_identifier() -> None:
    """Eight files, one contract.

    They stay eight files because the blueprint manifest reserves all eight paths and
    tests/integration/cross_language/test_event_envelopes.py reads one of them as a complete
    document; a `$ref` stub would break both. What changed is that one generator now writes all
    eight, so they cannot drift apart again.
    """
    bodies = set()
    for path in _envelope_paths():
        schema = json.loads(path.read_text(encoding="utf-8"))
        identifier = schema.pop("$id")
        domain = path.parent.parent.name
        assert identifier.endswith(f"/{domain}/v1/event-envelope.json"), path
        bodies.add(json.dumps(schema, sort_keys=True))
    assert len(bodies) == 1, "envelope copies disagree about the same protobuf message"


def test_every_generated_schema_is_a_distinct_document() -> None:
    """Distinct schema documents may not share a `$id`.

    The tree shipped with all eight copies claiming
    https://schemas.mindclade.dev/events/v1/envelope.json, so any registry that loaded more than
    one silently kept whichever it read last. protocols/events/validate_contracts.py already
    rejects a duplicate `$id` among the admission schemas; the generated tree now answers to the
    same rule.
    """
    identifiers = [
        json.loads(path.read_text(encoding="utf-8"))["$id"]
        for path in sorted(GENERATED.rglob("*.schema.json"))
    ]
    assert len(set(identifiers)) == len(identifiers)


def _payload_unions() -> dict[Path, list[str]]:
    """Every payload-union projection, as {generated schema path: declared message names}.

    Read from the policy a reviewer edits, not from the generated files, so the assertions below
    compare the tree to a declaration rather than to itself. The set equality is what stops a
    union schema being deleted, or a projection being added without a file appearing.
    """
    policy = _policy()
    output_root = (ROOT / str(policy["output_root"])).resolve()
    unions = {
        (output_root / str(projection["path"])).resolve(): list(projection["payload_messages"])
        for projection in policy["projections"]
        if projection.get("kind") == "payload_union"
    }
    assert unions, f"{GENERATOR.POLICY_RELPATH} declares no payload_union projection"
    assert set(unions) == {path.resolve() for path in GENERATED.rglob("*-events.schema.json")}
    return unions


def test_domain_payload_union_accepts_exactly_its_declared_messages() -> None:
    """An undeclared domain accepts nothing; a declared one accepts exactly what it declares.

    This assertion read `schema["not"] == {}` for EVERY union until orchestration/v1 declared
    payloads. It was only ever stricter than its own name — "undeclared" — because all eight
    `payload_messages` lists had been empty since the file was created, so "every union" and
    "every undeclared union" named the same set. Emptiness is now read from the policy instead
    of assumed of every domain.

    The ratchet on the seven still-undeclared domains is unchanged and still bites: `{}` — what
    these files used to hold — accepts every document, including one whose every field is
    wrong, so a domain with nothing declared must accept nothing. What is new is the positive
    half: a declared union must be exactly one `$ref` per declared name, in the declared order,
    with a definition behind each. Neither half can pass by accident of the other.
    """
    for path, names in sorted(_payload_unions().items()):
        schema = json.loads(path.read_text(encoding="utf-8"))
        if names:
            assert "not" not in schema, path
            assert schema["anyOf"] == [{"$ref": f"#/$defs/{name}"} for name in names], path
            assert set(schema["$defs"]) >= set(names), path
        else:
            assert "anyOf" not in schema, path
            assert schema["not"] == {}, path
            assert GENERATOR.POLICY_RELPATH in schema["description"], path


def _emitted_payload_keys() -> dict[str, tuple[frozenset[str], frozenset[str]]]:
    """{Go struct name: (always-emitted JSON keys, omitempty keys)} read from the store.

    Parsed from the `json:` struct tags rather than restated here, because a restatement is the
    thing that goes stale. Reaching across into a Go file from a codegen test is deliberate and
    is the point: a schema derived from a .proto can be internally consistent and still describe
    a document no producer writes, and the store's struct tags are the only place the emitted
    key set is actually decided.
    """
    text = ORCHESTRATION_STORE_EVENTS.read_text(encoding="utf-8")
    emitted: dict[str, tuple[frozenset[str], frozenset[str]]] = {}
    for name in sorted(set(ORCHESTRATION_FIXTURE_SOURCES.values())):
        match = re.search(rf"\ntype {name} struct \{{(.*?)\n\}}", text, re.DOTALL)
        assert match, f"{name} is no longer declared in {ORCHESTRATION_STORE_EVENTS}"
        tags = re.findall(r'json:"([^"]+)"', match.group(1))
        assert tags, f"{name} declares no JSON tags"
        emitted[name] = (
            frozenset(tag for tag in tags if ",omitempty" not in tag),
            frozenset(tag.split(",")[0] for tag in tags if ",omitempty" in tag),
        )
    return emitted


def test_orchestration_union_accepts_what_the_orchestration_store_emits() -> None:
    """The first declared union, checked against the bytes its producer actually writes.

    Two plausible modellings would have passed every other assertion in this module and still
    described documents nobody emits: `mindclade.common.v1.ResourceId` for the identifiers,
    which proto3 JSON writes as `{"value": "..."}` rather than as a bare string, and proto
    enums for the states, which it writes as `STAGE_STATE_RUNNING` rather than as the domain's
    own `running`. Only a fixture taken from the producer catches either.

    Both `omitempty` branches are covered: an attempt with and without a failure, and a
    run-scoped cancellation (which names no stage) alongside an attempt-scoped one (which must).
    """
    from configs.contract_validation import validate

    schema = json.loads(
        (GENERATED / "orchestration/v1/orchestration-events.schema.json").read_text(
            encoding="utf-8"
        )
    )
    emitted = _emitted_payload_keys()
    fixtures = sorted(ORCHESTRATION_FIXTURES.glob("*.valid.json"))
    assert {path.name for path in fixtures} == set(ORCHESTRATION_FIXTURE_SOURCES), (
        "every orchestration fixture must name the store struct it records"
    )
    seen: dict[str, set[str]] = {name: set() for name in emitted}
    for path in fixtures:
        document = json.loads(path.read_text(encoding="utf-8"))
        always, optional = emitted[ORCHESTRATION_FIXTURE_SOURCES[path.name]]
        keys = set(document)
        assert always <= keys, f"{path.name}: missing emitted key(s) {sorted(always - keys)}"
        assert keys <= always | optional, (
            f"{path.name}: key(s) {sorted(keys - always - optional)} are not emitted by "
            f"{ORCHESTRATION_STORE_EVENTS.name}"
        )
        seen[ORCHESTRATION_FIXTURE_SOURCES[path.name]] |= keys & optional
        errors = validate(document, schema)
        assert not errors, f"{path.name}: {[(item.path, item.message) for item in errors]}"
        # anyOf over open objects would accept whatever any one branch tolerated, so prove the
        # branches are closed: an unreviewed field must match no branch at all.
        assert validate({**document, "unreviewed_field": "forbidden"}, schema), path.name
    for name, (_, optional) in emitted.items():
        assert seen[name] == optional, (
            f"{name}: fixtures never exercise omitempty key(s) {sorted(optional - seen[name])}, "
            "so the schema is unproven for the document the store writes when they are set"
        )


def test_bytes_and_wide_integers_stay_inside_what_json_can_carry() -> None:
    schema = json.loads(_envelope_paths()[0].read_text(encoding="utf-8"))
    payload = schema["properties"]["payload"]
    assert payload["type"] == "string"
    assert payload["contentEncoding"] == "base64"
    assert payload["pattern"] == GENERATOR.BASE64_PATTERN
    for name in ("aggregate_version", "occurred_at_unix_millis"):
        assert schema["properties"][name]["maximum"] <= GENERATOR.SAFE_JSON_INTEGER


def test_projection_covers_scalar_enum_message_and_repeated_fields() -> None:
    """The projector is exercised on a real message that uses the whole field surface.

    mindclade.training.v1.CheckpointComponent carries a scalar, an enum, a message reference and
    a repeated field, so the four projection rules are pinned against real descriptor data
    rather than a fixture that could agree with a broken projector.
    """
    surface = _surface()
    message = "mindclade.training.v1.CheckpointComponent"
    referenced = "mindclade.common.v1.ArtifactRef"
    rendered = GENERATOR.build(ROOT, _minimal_policy(surface, [message]), surface)
    schema = json.loads(next(iter(rendered.values())))

    fields = {field["name"]: field for field in _descriptor_fields(surface, message)}
    assert set(schema["properties"]) == set(fields)

    enum_name = fields["kind"]["type"]
    enum_values = [
        value["name"]
        for package in surface["packages"].values()
        if enum_name in package["enums"]
        for value in package["enums"][enum_name]["values"]
    ]
    assert schema["properties"]["kind"] == {"enum": enum_values, "type": "string"}
    assert schema["properties"]["artifact"] == {"$ref": f"#/$defs/{referenced}"}
    assert set(schema["$defs"][referenced]["properties"]) == {
        field["name"] for field in _descriptor_fields(surface, referenced)
    }
    tensor_fqns = schema["properties"]["tensor_fqns"]
    assert tensor_fqns["type"] == "array"
    assert tensor_fqns["items"]["type"] == "string"
    assert "maxItems" in tensor_fqns
    assert "maxLength" in tensor_fqns["items"]


# ---------------------------------------------------------------------------------------
# fail-closed
# ---------------------------------------------------------------------------------------


def test_a_dropped_protobuf_field_fails_the_projection() -> None:
    surface = _surface()
    fields = _descriptor_fields(surface, ENVELOPE_MESSAGE)
    fields.remove(next(field for field in fields if field["name"] == "payload_digest"))
    with pytest.raises(GENERATOR.PolicyError, match="payload_digest"):
        GENERATOR.build(ROOT, _policy(), surface)


def test_an_added_protobuf_field_reaches_the_schema() -> None:
    surface = _surface()
    _descriptor_fields(surface, ENVELOPE_MESSAGE).append(
        {
            "label": "LABEL_OPTIONAL",
            "name": "replayed",
            "number": 15,
            "oneof": None,
            "proto3_optional": False,
            "type": "TYPE_BOOL",
        }
    )
    envelopes = [
        json.loads(content)
        for content in GENERATOR.build(ROOT, _policy(), surface).values()
        if json.loads(content).get("type") == "object"
    ]
    assert len(envelopes) == 8
    for schema in envelopes:
        assert schema["properties"]["replayed"] == {"type": "boolean"}


def test_a_retyped_protobuf_field_fails_the_projection() -> None:
    """A string refinement on a field the proto turned into an integer must not be ignored."""

    surface = _surface()
    field = next(
        item for item in _descriptor_fields(surface, ENVELOPE_MESSAGE) if item["name"] == "event_id"
    )
    field["type"] = "TYPE_UINT32"
    with pytest.raises(GENERATOR.PolicyError, match="pattern"):
        GENERATOR.build(ROOT, _policy(), surface)


def test_an_unbounded_string_fails_the_projection() -> None:
    policy = copy.deepcopy(_policy())
    del policy["messages"][ENVELOPE_MESSAGE]["fields"]["workspace_id"]["maxLength"]
    with pytest.raises(GENERATOR.PolicyError, match="maxLength"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_64_bit_integer_beyond_json_precision_fails_the_projection() -> None:
    # Dropping the declared maximum leaves the field with the protobuf uint64 range, which a
    # JSON number cannot carry: 2^64-1 does not survive a round trip through a double.
    policy = copy.deepcopy(_policy())
    del policy["messages"][ENVELOPE_MESSAGE]["fields"]["aggregate_version"]["maximum"]
    with pytest.raises(GENERATOR.PolicyError, match="JSON number"):
        GENERATOR.build(ROOT, policy, _surface())

    policy = copy.deepcopy(_policy())
    policy["messages"][ENVELOPE_MESSAGE]["fields"]["aggregate_version"]["maximum"] = 2**53
    with pytest.raises(GENERATOR.PolicyError, match="JSON number"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_refinement_that_widens_the_protobuf_range_fails_the_projection() -> None:
    policy = copy.deepcopy(_policy())
    policy["messages"][ENVELOPE_MESSAGE]["fields"]["aggregate_version"]["minimum"] = -1
    with pytest.raises(GENERATOR.PolicyError, match="widens the protobuf range"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_required_field_the_message_does_not_declare_fails_the_projection() -> None:
    policy = copy.deepcopy(_policy())
    policy["messages"][ENVELOPE_MESSAGE]["required"].append("retired_field")
    with pytest.raises(GENERATOR.PolicyError, match="retired_field"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_refinement_for_a_field_the_message_lost_fails_the_projection() -> None:
    policy = copy.deepcopy(_policy())
    policy["messages"][ENVELOPE_MESSAGE]["fields"]["retired_field"] = {"maxLength": 8}
    with pytest.raises(GENERATOR.PolicyError, match="no longer declares"):
        GENERATOR.build(ROOT, policy, _surface())


def test_an_unmapped_protobuf_type_fails_closed() -> None:
    surface = _surface()
    field = next(
        item for item in _descriptor_fields(surface, ENVELOPE_MESSAGE) if item["name"] == "payload"
    )
    field["type"] = "google.protobuf.Struct"
    with pytest.raises(GENERATOR.PolicyError, match="no projection rule"):
        GENERATOR.build(ROOT, _policy(), surface)


def test_a_map_field_fails_closed() -> None:
    """protoc's synthesized `*Entry` messages must not slip through the message branch.

    `map<K, V> m = 1;` reaches the descriptor surface as `repeated MEntry m = 1;` with the entry
    message flattened into the same table as declared ones and the `map_entry` option dropped.
    Projected as an ordinary message reference it yields an array of `{key, value}` pairs, which
    is not what proto3 JSON emits — so a schema that looked plausible would reject every real
    document. It refuses instead.
    """
    surface = _surface()
    message = "mindclade.orchestration.v1.CreateRunSpec"
    labels = next(
        field for field in _descriptor_fields(surface, message) if field["name"] == "labels"
    )
    assert labels["label"] == "LABEL_REPEATED"
    assert labels["type"].endswith("LabelsEntry"), "descriptor no longer models labels as a map"

    with pytest.raises(GENERATOR.PolicyError, match="map entry"):
        GENERATOR.build(ROOT, _minimal_policy(surface, [message]), surface)


def test_a_payload_union_accepts_a_document_valid_for_one_branch() -> None:
    """`oneOf` would reject the documents the union exists to accept.

    Every proto3 field is optional, so payload schemas overlap by construction: a document that
    satisfies a message carrying only `request_id` also satisfies one carrying `request_id`,
    `deployment_id` and `endpoint`. `oneOf` demands exactly one match and fails on both. This
    validates through the repository's own hermetic validator rather than by reading the
    keyword, so the assertion is about behaviour and not about spelling.
    """
    from configs.contract_validation import validate

    surface = _surface()
    narrow = "mindclade.inference.v1.InferenceAccepted"
    wide = "mindclade.runtime.v1.RuntimeDispatchResponse"
    policy = _minimal_policy(
        surface,
        [narrow, wide],
        projections=[
            {
                "kind": "payload_union",
                "path": "runtime/v1/runtime-events.schema.json",
                "payload_messages": [narrow, wide],
                "title": "Union under test",
            }
        ],
    )
    schema = json.loads(next(iter(GENERATOR.build(ROOT, policy, surface).values())))

    assert "oneOf" not in schema
    assert schema["anyOf"] == [{"$ref": f"#/$defs/{narrow}"}, {"$ref": f"#/$defs/{wide}"}]
    # Valid for both branches, which is exactly the case oneOf gets wrong.
    assert validate({"request_id": "req_1"}, schema) == ()
    # Valid for neither: every branch is a closed object.
    assert validate({"unreviewed_field": "x"}, schema)


def test_a_boolean_is_not_a_numeric_bound() -> None:
    """`isinstance(True, int)` is true, so `{"minimum": true}` used to reach the schema file."""

    policy = copy.deepcopy(_policy())
    policy["messages"][ENVELOPE_MESSAGE]["fields"]["aggregate_version"]["minimum"] = True
    with pytest.raises(GENERATOR.PolicyError, match="must be an integer"):
        GENERATOR.build(ROOT, policy, _surface())


def test_repeated_bounds_must_be_consistent() -> None:
    """An array whose minItems exceeds its maxItems can never be satisfied."""

    surface = _surface()
    message = "mindclade.training.v1.CheckpointComponent"
    policy = _minimal_policy(surface, [message])
    policy["messages"][message]["fields"]["tensor_fqns"].update({"maxItems": 2, "minItems": 9})
    with pytest.raises(GENERATOR.PolicyError, match="minItems 9 exceeds maxItems 2"):
        GENERATOR.build(ROOT, policy, surface)


def test_a_policy_message_the_descriptor_does_not_declare_fails_the_projection() -> None:
    policy = copy.deepcopy(_policy())
    policy["messages"]["mindclade.events.v1.RetiredEnvelope"] = {"fields": {}}
    with pytest.raises(GENERATOR.PolicyError, match="does not declare"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_policy_message_no_projection_reaches_fails_the_projection() -> None:
    """An unreachable entry is validated against nothing, so its refinements can rot."""

    policy = copy.deepcopy(_policy())
    policy["messages"]["mindclade.common.v1.ResourceId"] = {"fields": {"value": {"maxLength": 64}}}
    with pytest.raises(GENERATOR.PolicyError, match="never reached by a projection"):
        GENERATOR.build(ROOT, policy, _surface())


def test_a_renamed_projection_path_still_converges(tmp_path: Path) -> None:
    """Regeneration must fix what it can before complaining about what it cannot.

    The orphan check used to run before the write loop, so renaming a projection path made the
    generator write nothing at all and report an orphan — leaving the one command that is meant
    to repair the tree unable to repair anything until the stale file was deleted by hand.
    """
    policy = _policy()
    renamed = copy.deepcopy(policy)
    renamed["projections"] = [
        {
            "kind": "payload_union",
            "path": "runtime/v1/renamed-events.schema.json",
            "payload_messages": [],
            "title": "Renamed union",
        }
    ]
    renamed["messages"] = {}

    (tmp_path / "protocols/mappings").mkdir(parents=True)
    (tmp_path / "protocols/compatibility").mkdir(parents=True)
    (tmp_path / "protocols/events/generated/runtime/v1").mkdir(parents=True)
    (tmp_path / GENERATOR.POLICY_RELPATH).write_text(json.dumps(renamed), encoding="utf-8")
    (tmp_path / policy["descriptor_surface"]).write_text(json.dumps(_surface()), encoding="utf-8")
    stale = tmp_path / "protocols/events/generated/runtime/v1/runtime-events.schema.json"
    stale.write_text("{}\n", encoding="utf-8")

    code, messages = GENERATOR.generate(tmp_path, check=False)

    written = tmp_path / "protocols/events/generated/runtime/v1/renamed-events.schema.json"
    assert written.is_file(), "the rename target was not written"
    assert code == 1, "the stale file must still be reported"
    assert any("runtime-events.schema.json" in message for message in messages)
