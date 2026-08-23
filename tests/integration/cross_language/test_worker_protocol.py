# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Mechanical conformance between `mindclade.runtime.v1` and its language projections.

`libs/rust/worker_protocol` is a *hand-written* projection of the runtime protobuf
contract — the crate has no prost/protobuf dependency at all, so nothing in the
Rust build fails when a proto field is added, renamed, retyped, or dropped. Go's
`control/runtime_authority` is a second hand-written projection of the same
contract, and it is the *signer* while Rust is the *verifier*: if their MCCE1
canonical encodings disagree on one field name, order, or integer width, every
signature verifies as invalid at runtime and nothing in either language's own
test suite notices.

The previous version of this module asserted three substrings against the proto
text ("ExecutionTicket" in worker_command.proto, "digest" in
buffer_descriptor.proto, "bytes payload" absent). That passes through any amount
of drift in either projection, which is exactly the assurance gap this file now
closes. Everything below is derived from the sources; nothing is hard-coded
except the reviewed cross-language mapping tables, which are the contract.

Sibling helpers here (`proto_messages`, `proto_enums`) are imported by
`test_event_envelopes.py` and `test_manifest_roundtrip.py`; `conftest.py` makes
that bare-name import work under `--import-mode=importlib`.
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
PROTO_ROOT = ROOT / "protocols/proto/mindclade"
RUNTIME_V1 = PROTO_ROOT / "runtime/v1"
ORCHESTRATION_V1 = PROTO_ROOT / "orchestration/v1"
CRATE = ROOT / "libs/rust/worker_protocol"
GO_AUTHORITY = ROOT / "control/runtime_authority"

# --------------------------------------------------------------------------- #
# protobuf source parsing
# --------------------------------------------------------------------------- #

_MESSAGE = re.compile(r"\bmessage\s+(\w+)\s*\{")
_ENUM = re.compile(r"\benum\s+(\w+)\s*\{")
_ONEOF = re.compile(r"\boneof\s+(\w+)\s*\{")
# The type may be a `map<k, v>` and the declaration may carry `[options]`; both
# are dropped by a stricter pattern, and a dropped field is invisible to every
# comparison below. Nothing else in a message body ends in a semicolon at the
# top level, so `_fields` treats an unmatched statement as an error rather than
# skipping it.
_FIELD = re.compile(
    r"^(?:(repeated|optional)\s+)?([.\w]+(?:<[^>]*>)?)\s+(\w+)\s*=\s*(\d+)\s*(?:\[[^\]]*\])?$"
)
_ENUM_VALUE = re.compile(r"^(\w+)\s*=\s*(-?\d+)\s*(?:\[[^\]]*\])?$")
# Statements that are legitimately not fields. `reserved` and `option` are the
# only ones this contract uses; anything else unrecognised is a parser gap.
_NON_FIELD = re.compile(r"^(reserved|option|extensions)\b")


def _blocks(text: str, pattern: re.Pattern[str]) -> list[tuple[str, str]]:
    """(name, body) for every brace-balanced block matching `pattern`, nested included.

    Nested blocks are returned too. Skipping them would let a `bytes payload`
    field hide inside a nested message, out of reach of the checks below.
    """
    found: list[tuple[str, str]] = []
    for match in pattern.finditer(text):
        opening = text.find("{", match.start())
        depth = 1
        index = opening + 1
        while index < len(text) and depth:
            if text[index] == "{":
                depth += 1
            elif text[index] == "}":
                depth -= 1
            index += 1
        if depth != 0:
            raise AssertionError(f"unbalanced braces for {match.group(1)}")
        found.append((match.group(1), text[opening + 1 : index - 1]))
    return found


def _statements(body: str) -> list[str]:
    """Semicolon-terminated statements at the top nesting level of `body`.

    The accumulator is cleared when a nested block closes. Without that, the
    `enum AccessMode` / `oneof command` header text stays buffered and is glued
    onto the next field declaration, so the first field after any nested block
    parses as neither a field nor a recognised statement.
    """
    statements: list[str] = []
    current: list[str] = []
    depth = 0
    for character in body:
        if character == "{":
            depth += 1
        elif character == "}":
            depth -= 1
            if depth == 0:
                current.clear()
        elif depth == 0:
            if character == ";":
                if statement := " ".join("".join(current).split()):
                    statements.append(statement)
                current.clear()
            else:
                current.append(character)
    return statements


def _fields(owner: str, body: str) -> dict[str, dict[str, object]]:
    parsed: dict[str, dict[str, object]] = {}
    for statement in _statements(body):
        match = _FIELD.match(statement)
        if match is None:
            if _NON_FIELD.match(statement):
                continue
            raise AssertionError(f"{owner}: unparsed protobuf statement {statement!r}")
        label, declared, name, tag = match.groups()
        parsed[name] = {
            "type": declared,
            "label": label or "singular",
            "tag": int(tag),
        }
    return parsed


def _proto_sources(directory: Path) -> list[tuple[Path, str]]:
    return [
        (path, re.sub(r"//.*", "", path.read_text())) for path in sorted(directory.glob("*.proto"))
    ]


def proto_messages(directory: Path) -> dict[str, dict[str, dict[str, object]]]:
    """message name -> field name -> {type, label, tag}, nested messages included.

    Fields declared inside a `oneof` are reported by `proto_oneofs` instead, so a
    message's field map is the set of fields that are always present.
    """
    messages: dict[str, dict[str, dict[str, object]]] = {}
    for path, text in _proto_sources(directory):
        for name, body in _blocks(text, _MESSAGE):
            if name in messages:
                raise AssertionError(f"duplicate message {name} in {path}")
            messages[name] = _fields(name, body)
    return messages


def proto_oneofs(directory: Path) -> dict[str, dict[str, dict[str, dict[str, object]]]]:
    """message name -> oneof name -> arm field name -> {type, label, tag}."""
    oneofs: dict[str, dict[str, dict[str, dict[str, object]]]] = {}
    for _, text in _proto_sources(directory):
        for name, body in _blocks(text, _MESSAGE):
            arms = {group: _fields(group, arm) for group, arm in _blocks(body, _ONEOF)}
            if arms:
                oneofs[name] = arms
    return oneofs


def _enum_values(body: str) -> list[str]:
    values: list[str] = []
    for statement in _statements(body):
        match = _ENUM_VALUE.match(statement)
        if match is None:
            if _NON_FIELD.match(statement):
                continue
            raise AssertionError(f"unparsed protobuf enum statement {statement!r}")
        values.append(match.group(1))
    return values


def proto_enums(directory: Path) -> dict[str, list[str]]:
    """enum name -> declared value names, in declaration order.

    Nested enums are keyed `Message.Enum`; top-level enums by their own name.
    """
    enums: dict[str, list[str]] = {}
    for _, text in _proto_sources(directory):
        nested: set[str] = set()
        for message, body in _blocks(text, _MESSAGE):
            for name, enum_body in _blocks(body, _ENUM):
                nested.add(name)
                enums[f"{message}.{name}"] = _enum_values(enum_body)
        for name, enum_body in _blocks(text, _ENUM):
            if name in nested:
                continue
            enums[name] = _enum_values(enum_body)
    return enums


# --------------------------------------------------------------------------- #
# Rust source parsing
# --------------------------------------------------------------------------- #

_RUST_STRUCT = re.compile(r"^pub struct (\w+) \{$")
_RUST_ENUM = re.compile(r"^pub enum (\w+) \{$")
_RUST_FIELD = re.compile(r"^    pub (\w+): (.+),$")
_RUST_VARIANT = re.compile(r"^    (\w+)\s*(\{|,)")
_RUST_VARIANT_FIELD = re.compile(r"^        (\w+): (.+),$")


def rust_structs(directory: Path) -> dict[str, dict[str, str]]:
    """`pub struct` name -> public field name -> declared Rust type."""
    structs: dict[str, dict[str, str]] = {}
    for path in sorted(directory.glob("*.rs")):
        lines = path.read_text().splitlines()
        index = 0
        while index < len(lines):
            if match := _RUST_STRUCT.match(lines[index]):
                fields: dict[str, str] = {}
                index += 1
                while index < len(lines) and lines[index] != "}":
                    if field := _RUST_FIELD.match(lines[index]):
                        fields[field.group(1)] = field.group(2)
                    index += 1
                structs[match.group(1)] = fields
            index += 1
    return structs


def rust_enums(directory: Path) -> dict[str, dict[str, dict[str, str]]]:
    """`pub enum` name -> variant name -> struct-variant field name -> Rust type.

    A unit variant maps to an empty field dict.
    """
    enums: dict[str, dict[str, dict[str, str]]] = {}
    for path in sorted(directory.glob("*.rs")):
        lines = path.read_text().splitlines()
        index = 0
        while index < len(lines):
            if match := _RUST_ENUM.match(lines[index]):
                variants: dict[str, dict[str, str]] = {}
                index += 1
                current: str | None = None
                while index < len(lines) and lines[index] != "}":
                    if variant := _RUST_VARIANT.match(lines[index]):
                        current = variant.group(1)
                        variants[current] = {}
                    elif current and (field := _RUST_VARIANT_FIELD.match(lines[index])):
                        variants[current][field.group(1)] = field.group(2)
                    index += 1
                enums[match.group(1)] = variants
            index += 1
    return enums


# --------------------------------------------------------------------------- #
# MCCE1 canonical-encoding extraction (Rust verifier and Go signer)
# --------------------------------------------------------------------------- #

# The signed form is the cross-language contract: Go signs these bytes and Rust
# verifies them, so a difference in document kind, field key, field order, or
# integer width is a production authentication failure, not a style difference.
_GO_ENCODER = re.compile(r"(\w+)\s*:?=\s*newCanonicalEncoder\(\"([a-z0-9-]+)\"\)")
_GO_WRITE = re.compile(r"\b(\w+)\.(text|u32|u64|boolean|stringSet|nested)\(\"([a-z0-9_]+)\"")
_RUST_ENCODER = re.compile(r"let mut (\w+) = CanonicalEncoder::new\(\"([a-z0-9-]+)\"\)")
_RUST_WRITE = re.compile(
    r"\b(\w+)\.(text|u32|u64|boolean|strings|nested)\(\s*\n?\s*\"([a-z0-9_]+)\""
)

# One vocabulary for both languages: Go calls a sorted string list `stringSet`,
# Rust calls it `strings`, and they emit identical bytes.
_WIDTH = {
    "text": "text",
    "u32": "u32",
    "u64": "u64",
    "boolean": "bool",
    "nested": "nested",
    "stringSet": "set",
    "strings": "set",
}


def _canonical(paths: list[Path], open_re: re.Pattern[str], write_re: re.Pattern[str]):
    """document kind -> ordered [(width, field key)] written into that document."""
    documents: dict[str, list[tuple[str, str]]] = {}
    for path in paths:
        text = path.read_text()
        marks: list[tuple[int, str, str, str]] = []
        for match in open_re.finditer(text):
            marks.append((match.start(), "open", match.group(1), match.group(2)))
        for match in write_re.finditer(text):
            marks.append((match.start(), _WIDTH[match.group(2)], match.group(1), match.group(3)))
        marks.sort()
        # variable name -> document kind, so two encoders alive at once (a route
        # entry inside the route snapshot loop) do not interleave their fields.
        binding: dict[str, str] = {}
        for _, width, variable, name in marks:
            if width == "open":
                binding[variable] = name
                documents.setdefault(name, [])
            elif (kind := binding.get(variable)) is not None:
                written = documents[kind]
                # `snapshot_digest` is written from both arms of an if/else in each
                # language; the same key twice in a row is one wire field.
                if not written or written[-1] != (width, name):
                    written.append((width, name))
    return documents


def rust_canonical():
    return _canonical(sorted((CRATE / "src").glob("*.rs")), _RUST_ENCODER, _RUST_WRITE)


def go_canonical():
    # Production encoders only. A Go unit test that builds a partial document
    # would otherwise append its fields to the production map and surface as a
    # mismatch pointing at `control/runtime_authority`, not at the test.
    sources = [
        path for path in sorted(GO_AUTHORITY.glob("*.go")) if not path.name.endswith("_test.go")
    ]
    return _canonical(sources, _GO_ENCODER, _GO_WRITE)


# --------------------------------------------------------------------------- #
# the reviewed contract mapping
# --------------------------------------------------------------------------- #

# proto message -> Rust type -> {proto field: Rust field}. Every proto field must
# appear and every public Rust field must be claimed; a rename on either side
# fails here rather than silently producing two incompatible views.
PROJECTION: dict[str, tuple[str, dict[str, str]]] = {
    "DetachedSignature": (
        "DetachedSignature",
        {"algorithm": "algorithm", "key_id": "key_id", "value": "value"},
    ),
    "ArtifactGrant": (
        "ArtifactGrant",
        {
            "readable_digests": "readable_digests",
            "writable_namespaces": "writable_namespaces",
            "maximum_read_bytes": "maximum_read_bytes",
            "maximum_write_bytes": "maximum_write_bytes",
            "allow_range_reads": "allow_range_reads",
            "allow_multipart_writes": "allow_multipart_writes",
        },
    ),
    "ExecutionTicketClaims": (
        "ExecutionTicketClaims",
        {
            "ticket_id": "ticket_id",
            "issuer": "issuer",
            "tenant_id": "tenant_id",
            "workspace_id": "workspace_id",
            "run_id": "run_id",
            "job_id": "job_id",
            "stage_id": "stage_id",
            "request_id": "request_id",
            "attempt": "attempt",
            "fencing_token": "fencing_token",
            "model_bundle_digest": "model_bundle",
            "engine_bundle_digest": "engine_bundle",
            "resolved_config_digest": "resolved_config_digest",
            "reference_snapshot_digest": "reference_snapshot",
            "artifacts": "artifacts",
            "budget": "budget",
            "execution_class": "execution_class",
            "accelerator_capability": "accelerator_capability",
            "not_before_unix_millis": "not_before_unix_millis",
            "deadline_unix_millis": "deadline_unix_millis",
            "expires_unix_millis": "expires_unix_millis",
            "policy_epoch": "policy_epoch",
            "route_snapshot_version": "route_snapshot_version",
            "revocation_epoch": "revocation_epoch",
            "idempotency_key": "idempotency_key",
        },
    ),
    "ExecutionTicket": (
        "ExecutionTicket",
        {"claims": "claims", "signature": "signature"},
    ),
    "AdmissionGrantClaims": (
        "AdmissionGrantClaims",
        {
            "grant_id": "grant_id",
            "tenant_id": "tenant_id",
            "principal_id": "principal_id",
            "allowed_deployments": "allowed_deployments",
            "allowed_capabilities": "allowed_capabilities",
            "region": "region",
            "maximum_concurrency": "maximum_concurrency",
            "maximum_requests": "maximum_requests",
            "maximum_input_units": "maximum_input_units",
            "maximum_output_units": "maximum_output_units",
            "not_before_unix_millis": "not_before_unix_millis",
            "expires_unix_millis": "expires_unix_millis",
            "policy_epoch": "policy_epoch",
            "revocation_epoch": "revocation_epoch",
        },
    ),
    "AdmissionGrant": ("AdmissionGrant", {"claims": "claims", "signature": "signature"}),
    "BufferDescriptor": (
        "BufferDescriptor",
        {
            "segment_id": "segment_id",
            "generation": "generation",
            # offset_bytes/length_bytes are carried together in a ByteRange, which
            # is what makes `start + length` overflow a construction error instead
            # of a downstream surprise.
            "offset_bytes": "range",
            "length_bytes": "range",
            "element_type": "element_type",
            "shape": "shape",
            "content_digest": "digest",
            "owner_process": "owner_process",
            "lease_expires_unix_millis": "lease_expires_unix_millis",
            "access_mode": "access",
            "transport": "transport",
            "locator": "locator",
        },
    ),
    "WorkerStatus": (
        "WorkerStatus",
        {
            "sequence": "sequence",
            "ticket_id": "ticket_id",
            "fencing_token": "fencing_token",
            "state": "state",
            "observed_unix_millis": "observed_unix_millis",
            "message": "message",
            "outputs": "outputs",
            "diagnostic_artifact_digest": "diagnostic_artifact",
        },
    ),
    "DeploymentRoute": (
        "DeploymentRoute",
        {
            "deployment_id": "deployment_id",
            "model_bundle_digest": "model_bundle",
            "engine_bundle_digest": "engine_bundle",
            "endpoint": "endpoint",
            "region": "region",
            "weight": "weight",
            "capabilities": "capabilities",
            "lease_expires_unix_millis": "lease_expires_unix_millis",
            "safety_policy_digest": "safety_policy",
        },
    ),
    "RouteSnapshotClaims": (
        "RouteSnapshotClaims",
        {
            "snapshot_id": "snapshot_id",
            "snapshot_digest": "snapshot_digest",
            "version": "version",
            "policy_epoch": "policy_epoch",
            "revocation_epoch": "revocation_epoch",
            "created_unix_millis": "created_unix_millis",
            "expires_unix_millis": "expires_unix_millis",
            "routes": "routes",
            "minimum_runtime_version": "minimum_runtime_version",
        },
    ),
    "RouteSnapshot": ("RouteSnapshot", {"claims": "claims", "signature": "signature"}),
    "RevocationSnapshotClaims": (
        "RevocationSnapshotClaims",
        {
            "epoch": "epoch",
            "created_unix_millis": "created_unix_millis",
            "expires_unix_millis": "expires_unix_millis",
            "revoked_grant_ids": "revoked_grant_ids",
            "revoked_ticket_ids": "revoked_ticket_ids",
            "revoked_deployment_ids": "revoked_deployment_ids",
            "revoked_bundle_digests": "revoked_bundle_digests",
        },
    ),
    "RevocationSnapshot": (
        "RevocationSnapshot",
        {"claims": "claims", "signature": "signature"},
    ),
}

# ExecutionBudget is projected onto a `ResourceVector` rather than fourteen
# scalar fields, so it has no per-field Rust struct member to compare. Its whole
# accounting is the MCCE1 document below: every proto field appears there at its
# proto integer width, which is what the signature is taken over.
BUDGET_RUST_FIELDS = {"resources", "maximum_output_bytes"}

# Messages that are RPC envelopes for the gateway service edge rather than
# node-local worker types the crate projects. Listing them here is not an
# exemption: a new runtime/v1 message that is in neither table fails the
# partition assertion below and forces an explicit decision.
SERVICE_ENVELOPE_MESSAGES = {
    "GetRevocationsRequest",
    "GetRouteSnapshotRequest",
    "RuntimeDispatchRequest",
    "RuntimeDispatchResponse",
    "RuntimeExecuteRequest",
    # WorkerCommand and its oneof arms are projected as a single Rust enum, so
    # test_worker_command_oneof_matches_rust_enum checks them in full — field
    # set, integer width, repeated shape, and time unit — instead of this table.
    "WorkerCommand",
    "StartCommand",
    "CancelCommand",
    "DrainCommand",
    "HeartbeatCommand",
}

# proto integer type -> the Rust types allowed to carry it. A newtype is allowed
# only when it is at least as narrow as the wire type; FencingToken wraps a u64.
INTEGER_WIDTHS = {
    "uint32": {"u32"},
    "uint64": {"u64", "FencingToken", "ByteRange"},
}

# proto enum -> Rust enum, and the proto value prefix that its members share.
ENUM_PROJECTION = {
    "BufferDescriptor.AccessMode": ("BufferAccess", "ACCESS_MODE_"),
    "BufferDescriptor.Transport": ("BufferTransport", "TRANSPORT_"),
    "WorkerState": ("WorkerState", "WORKER_STATE_"),
}

# Rust spells TRANSPORT_ARTIFACT_REF as `Artifact`: the Rust value names a
# transport, and `ArtifactRef` would read as the common/v1 message type.
ENUM_VALUE_RENAMES = {("BufferTransport", "ArtifactRef"): "Artifact"}


def _pascal(value: str) -> str:
    return "".join(part.capitalize() for part in value.split("_"))


def _element_type(declared: str) -> str:
    """`Vec<u64>` -> `u64`; a scalar declaration is returned unchanged.

    A repeated wire field constrains the *element* width, not the container.
    """
    for container in ("Vec<", "BTreeSet<"):
        if declared.startswith(container) and declared.endswith(">"):
            return declared[len(container) : -1]
    return declared


@pytest.fixture(scope="module")
def runtime_proto():
    return proto_messages(RUNTIME_V1)


@pytest.fixture(scope="module")
def runtime_enums():
    return proto_enums(RUNTIME_V1)


@pytest.fixture(scope="module")
def rust():
    return rust_structs(CRATE / "src"), rust_enums(CRATE / "src")


@pytest.fixture(scope="module")
def canonical():
    return rust_canonical(), go_canonical()


def test_every_runtime_message_is_classified(runtime_proto):
    """No runtime/v1 message may be neither projected nor declared service-only."""
    classified = set(PROJECTION) | SERVICE_ENVELOPE_MESSAGES | {"ExecutionBudget"}
    assert set(runtime_proto) == classified, (
        "runtime/v1 message set changed; classify it in PROJECTION or "
        f"SERVICE_ENVELOPE_MESSAGES: {sorted(set(runtime_proto) ^ classified)}"
    )


@pytest.mark.parametrize("message", sorted(PROJECTION))
def test_rust_projection_covers_every_proto_field(message, runtime_proto, rust):
    structs, _ = rust
    rust_name, mapping = PROJECTION[message]
    assert rust_name in structs, f"{rust_name} is no longer a public Rust struct"
    proto_fields = runtime_proto[message]
    assert set(mapping) == set(proto_fields), (
        f"{message}: proto fields and the declared projection disagree: "
        f"{sorted(set(mapping) ^ set(proto_fields))}"
    )
    claimed = set(mapping.values())
    assert claimed == set(structs[rust_name]), (
        f"{rust_name}: Rust fields and the declared projection disagree: "
        f"{sorted(claimed ^ set(structs[rust_name]))}"
    )


@pytest.mark.parametrize("message", sorted(PROJECTION))
def test_integer_widths_match_the_wire_type(message, runtime_proto, rust):
    """A uint32 wire field may not widen to u64 in Rust, or the reverse.

    Widening is not harmless here: the MCCE1 encoder writes four bytes for a u32
    and eight for a u64, so a width change silently invalidates every signature.
    """
    structs, _ = rust
    rust_name, mapping = PROJECTION[message]
    for field, spec in runtime_proto[message].items():
        allowed = INTEGER_WIDTHS.get(str(spec["type"]))
        # An unmapped field is reported by test_rust_projection_covers_every_proto_field
        # with a message that names it; raising a bare KeyError here instead would
        # bury that behind a stack trace.
        if allowed is None or field not in mapping:
            continue
        declared = _element_type(structs[rust_name][mapping[field]])
        assert declared in allowed, (
            f"{message}.{field} is {spec['type']} on the wire but "
            f"{rust_name}.{mapping[field]} is {declared}"
        )


@pytest.mark.parametrize("message", sorted(PROJECTION))
def test_repeated_fields_stay_collections(message, runtime_proto, rust):
    structs, _ = rust
    rust_name, mapping = PROJECTION[message]
    for field, spec in runtime_proto[message].items():
        if field not in mapping:
            continue
        declared = structs[rust_name][mapping[field]]
        # `bytes` is a scalar on the wire even though Rust spells it Vec<u8>;
        # treating it as repeated would demand a collection bound on a field
        # that is already length-bounded as an opaque value.
        if spec["type"] == "bytes":
            assert declared == "Vec<u8>", f"{message}.{field} is bytes but Rust holds {declared}"
            continue
        collection = declared.startswith(("Vec<", "BTreeSet<"))
        assert collection == (spec["label"] == "repeated"), (
            f"{message}.{field} is {spec['label']} on the wire but "
            f"{rust_name}.{mapping[field]} is {declared}"
        )


def test_worker_command_oneof_matches_rust_enum(runtime_proto, rust):
    """The command oneof and the Rust enum must agree on arms, fields, and types.

    These five messages are the one part of the projection that is an enum
    rather than a struct, so they are checked here in full — field set, integer
    width, repeated shape, and time unit — rather than only by name. Checking
    names alone would let `deadline_unix_millis` narrow to u32, or `inputs` stop
    being a collection, inside a message this module claims to cover.
    """
    _, enums = rust
    arms = proto_oneofs(RUNTIME_V1)["WorkerCommand"]["command"]
    variants = enums["WorkerCommand"]
    assert {_pascal(arm) for arm in arms} == set(variants), (
        f"worker command arms differ: {sorted({_pascal(a) for a in arms} ^ set(variants))}"
    )
    # `sequence` sits on the enclosing message on the wire and is inlined into
    # every Rust variant, so each variant carries its arm's fields plus sequence.
    sequence = runtime_proto["WorkerCommand"]["sequence"]
    for arm, spec in arms.items():
        arm_fields = dict(runtime_proto[str(spec["type"])], sequence=sequence)
        variant = variants[_pascal(arm)]
        assert set(variant) == set(arm_fields), (
            f"WorkerCommand::{_pascal(arm)} fields differ from {spec['type']}: "
            f"{sorted(set(variant) ^ set(arm_fields))}"
        )
        for field, field_spec in arm_fields.items():
            declared = variant[field]
            allowed = INTEGER_WIDTHS.get(str(field_spec["type"]))
            if allowed is not None:
                assert _element_type(declared) in allowed, (
                    f"WorkerCommand::{_pascal(arm)}.{field} is {field_spec['type']} on the "
                    f"wire but {declared} in Rust"
                )
            assert declared.startswith(("Vec<", "BTreeSet<")) == (
                field_spec["label"] == "repeated"
            ), f"WorkerCommand::{_pascal(arm)}.{field} shape differs from the wire"
            if field.endswith("_unix_millis"):
                assert declared == "u64"


@pytest.mark.parametrize("proto_enum", sorted(ENUM_PROJECTION))
def test_enum_value_sets_match_and_reject_the_zero_value(proto_enum, rust, runtime_enums):
    """Rust must carry every named wire value and must not carry UNSPECIFIED.

    proto3 hands an unknown or unset enum back as value 0. A Rust variant for it
    would let an unset field flow into policy as if it were a real state, so the
    projection deliberately has no such variant and decoders must fail closed.
    """
    _, enums = rust
    rust_name, prefix = ENUM_PROJECTION[proto_enum]
    values = runtime_enums[proto_enum]
    assert values[0] == f"{prefix}UNSPECIFIED", f"{proto_enum} must reserve 0 for UNSPECIFIED"
    expected = set()
    for value in values[1:]:
        assert value.startswith(prefix), f"{proto_enum}.{value} does not share the value prefix"
        name = _pascal(value.removeprefix(prefix))
        expected.add(ENUM_VALUE_RENAMES.get((rust_name, name), name))
    assert expected == set(enums[rust_name]), (
        f"{proto_enum} and Rust {rust_name} disagree: {sorted(expected ^ set(enums[rust_name]))}"
    )


def test_stage_kind_taxonomy_agrees_across_all_four_languages(rust):
    """The stage taxonomy is one vocabulary; four copies of it must name it alike.

    Go (`control/orchestration`), Python (`libs/python/worker_runtime`), and the
    TypeScript SDK all derive their members from `StageKind` in
    orchestration/v1. The Rust `WorkloadKind` is a fourth hand-written copy, and
    nothing built compares it with the others.
    """
    proto_values = proto_enums(ORCHESTRATION_V1)["StageKind"]
    assert proto_values[0] == "STAGE_KIND_UNSPECIFIED"
    canonical = [value.removeprefix("STAGE_KIND_").lower() for value in proto_values[1:]]

    go_source = (ROOT / "control/orchestration/stage.go").read_text()
    go_values = re.findall(r"^\tStage\w+\s+StageKind = \"(\w+)\"$", go_source, re.MULTILINE)
    assert go_values == canonical, f"Go StageKind drifted from the proto: {go_values}"

    python_source = (ROOT / "libs/python/worker_runtime/contracts.py").read_text()
    python_body = python_source.split("class StageKind(StrEnum):", 1)[1]
    python_values = re.findall(r"^    \w+ = \"(\w+)\"$", python_body.split("\n\n", 1)[0], re.M)
    assert python_values == canonical, f"Python StageKind drifted from the proto: {python_values}"

    rust_values = list(rust[1]["WorkloadKind"])
    assert rust_values == [_pascal(value) for value in canonical], (
        "Rust WorkloadKind drifted from the canonical StageKind taxonomy: "
        f"{rust_values} != {[_pascal(value) for value in canonical]}"
    )


def test_timestamps_are_unix_milliseconds_everywhere(runtime_proto, rust):
    """One time unit across the contract, named in the field so it cannot be guessed."""
    structs, _ = rust
    forbidden = ("_unix_seconds", "_seconds", "_micros", "_nanos", "_at_seconds")
    for message, fields in runtime_proto.items():
        for field in fields:
            assert not field.endswith(forbidden), (
                f"{message}.{field} uses a non-millisecond time unit"
            )
    for message, (rust_name, mapping) in PROJECTION.items():
        for field, target in mapping.items():
            if field.endswith("_unix_millis"):
                assert target.endswith("_unix_millis"), (
                    f"{message}.{field} maps to {rust_name}.{target}, "
                    "which does not name the millisecond unit"
                )
                assert structs[rust_name][target] == "u64"


def test_canonical_documents_agree_between_the_go_signer_and_the_rust_verifier(canonical):
    """Go signs MCCE1 bytes and Rust verifies them; the encoders must be identical.

    The Python golden in `test_execution_ticket_golden.py` pins one document.
    This pins all of them, including the route, revocation, and admission
    documents that no golden covers — a reordered field there authenticates
    nothing and fails closed in production with no test to explain why.
    """
    rust_documents, go_documents = canonical
    assert set(rust_documents) == set(go_documents), (
        f"MCCE1 document kinds differ: {sorted(set(rust_documents) ^ set(go_documents))}"
    )
    for kind in sorted(rust_documents):
        assert rust_documents[kind] == go_documents[kind], (
            f"MCCE1 {kind!r} encodes differently in Rust and Go:\n"
            f"  rust={rust_documents[kind]}\n  go=  {go_documents[kind]}"
        )


CANONICAL_DOCUMENT_FOR = {
    "artifact-grant": "ArtifactGrant",
    "execution-budget": "ExecutionBudget",
    "execution-ticket-claims": "ExecutionTicketClaims",
    "admission-grant-claims": "AdmissionGrantClaims",
    "route-snapshot-claims": "RouteSnapshotClaims",
    "revocation-snapshot-claims": "RevocationSnapshotClaims",
    "deployment-route": "DeploymentRoute",
}

# `route-list-entry` is a framing wrapper, not a message projection: it exists so
# the repeated `routes` field signs as one nested value per element instead of a
# concatenation whose element boundaries a forger could shift. It has no proto
# message of its own, which is why it is named here rather than mapped above.
CANONICAL_FRAMING_DOCUMENTS = {"route-list-entry": [("nested", "route")]}


def test_every_signed_document_is_classified(canonical):
    """A new MCCE1 document must be compared with a proto message or declared framing.

    Without this the field-and-width comparison below silently covers only the
    documents someone remembered to list, which is the same hole this module was
    written to close on the proto side.
    """
    rust_documents, _ = canonical
    classified = set(CANONICAL_DOCUMENT_FOR) | set(CANONICAL_FRAMING_DOCUMENTS)
    assert set(rust_documents) == classified, (
        "MCCE1 document kinds changed; map it to a proto message in "
        "CANONICAL_DOCUMENT_FOR or declare it framing in CANONICAL_FRAMING_DOCUMENTS: "
        f"{sorted(set(rust_documents) ^ classified)}"
    )
    for kind, expected in CANONICAL_FRAMING_DOCUMENTS.items():
        assert rust_documents[kind] == expected, (
            f"MCCE1 framing document {kind!r} changed shape: {rust_documents[kind]}"
        )


# MCCE1 keys that intentionally differ from the proto field name. Both languages
# already agree on these; the mapping exists so the *proto* side is still checked.
CANONICAL_KEY_RENAMES = {
    ("execution-ticket-claims", "artifact_grant"): "artifacts",
}

# proto declared type -> the MCCE1 width that must carry it.
CANONICAL_WIDTH_FOR = {
    "string": "text",
    "uint32": "u32",
    "uint64": "u64",
    "bool": "bool",
}


@pytest.mark.parametrize("document", sorted(CANONICAL_DOCUMENT_FOR))
def test_canonical_documents_cover_the_proto_message_at_the_proto_width(
    document, runtime_proto, canonical
):
    """Every signed field is a proto field, at the proto's own integer width."""
    written = canonical[0][document]
    message = CANONICAL_DOCUMENT_FOR[document]
    proto_fields = runtime_proto[message]
    seen: dict[str, str] = {}
    for width, key in written:
        name = CANONICAL_KEY_RENAMES.get((document, key), key)
        assert name in proto_fields, f"MCCE1 {document} signs {key!r}, absent from {message}"
        seen[name] = width
    assert set(seen) == set(proto_fields), (
        f"MCCE1 {document} and {message} disagree on fields: "
        f"{sorted(set(seen) ^ set(proto_fields))}"
    )
    for name, width in seen.items():
        spec = proto_fields[name]
        if spec["label"] == "repeated":
            # A repeated scalar signs as a sorted string set; a repeated message
            # signs as one nested field holding the concatenated sub-documents,
            # because a message has no total order to sort by.
            expected = "set" if spec["type"] in CANONICAL_WIDTH_FOR else "nested"
        else:
            expected = CANONICAL_WIDTH_FOR.get(str(spec["type"]), "nested")
        assert width == expected, (
            f"MCCE1 {document}.{name} is signed as {width} but the wire type is "
            f"{spec['label']} {spec['type']} (expected {expected})"
        )


def _compact(source: str) -> str:
    """Whitespace-stripped source, so rustfmt's line wrapping cannot hide a call."""
    return re.sub(r"\s+", "", source)


def test_execution_budget_is_fully_accounted_for(runtime_proto, rust, canonical):
    """ExecutionBudget collapses onto a ResourceVector; nothing may fall out.

    The struct has no per-field member to diff, so its whole field accounting is
    the signed document: all fourteen proto fields, each at its wire width, and
    each u32 field taken through the checked conversion. A `as u32` cast in its
    place would truncate a >2^32 policy value and sign a smaller budget than the
    issuer authorised, so the assertion names `u32_value(` rather than merely
    checking that a u32 key is written.
    """
    structs, _ = rust
    assert set(structs["ExecutionBudget"]) == BUDGET_RUST_FIELDS
    signed = {key for _, key in canonical[0]["execution-budget"]}
    assert signed == set(runtime_proto["ExecutionBudget"]), (
        "execution-budget signs a different field set than the proto: "
        f"{sorted(signed ^ set(runtime_proto['ExecutionBudget']))}"
    )
    compact = _compact((CRATE / "src/lib.rs").read_text())
    for field, spec in runtime_proto["ExecutionBudget"].items():
        if spec["type"] == "uint32":
            assert f'encoder.u32("{field}",u32_value(' in compact, (
                f"execution budget {field} is not encoded through the checked u32 conversion"
            )
        else:
            assert f'encoder.u64("{field}",' in compact, (
                f"execution budget {field} is not encoded at its uint64 wire width"
            )
    assert "letu32_value=|kind:ResourceKind|->FaultResult<u32>{u32::try_from(" in compact, (
        "the execution-budget u32 conversion is no longer a checked try_from"
    )
    # validate() rejects an out-of-range value before anything signs it, so an
    # over-large budget fails closed rather than being narrowed at encode time.
    assert "ifself.resources.get(kind)>u64::from(u32::MAX){" in compact, (
        "execution budget validation no longer range-checks its u32 fields"
    )


# The exact bound each peer-controlled collection must meet, keyed by the Rust
# type that owns it. A substring match on `<field>.len() >` alone is satisfied by
# any unrelated local or closure parameter of the same name — deleting
# DetachedSignature's own bound still matched three `|value| value.len() > 256`
# closures elsewhere in the crate — so the expression is named in full.
COLLECTION_BOUNDS = {
    ("DetachedSignature", "value"): "self.value.len()>MAX_SIGNATURE_BYTES",
    ("ArtifactGrant", "readable_digests"): "self.readable_digests.len()>MAX_SET_ENTRIES",
    ("ArtifactGrant", "writable_namespaces"): "self.writable_namespaces.len()>4096",
    ("AdmissionGrantClaims", "allowed_deployments"): (
        "self.allowed_deployments.len()>MAX_SET_ENTRIES"
    ),
    ("AdmissionGrantClaims", "allowed_capabilities"): (
        "self.allowed_capabilities.len()>MAX_CAPABILITIES"
    ),
    ("BufferDescriptor", "shape"): "self.shape.len()>MAX_INPUT_DIMENSIONS",
    ("WorkerStatus", "outputs"): "status.outputs.len()>maximum_outputs",
    ("DeploymentRoute", "capabilities"): "self.capabilities.len()>MAX_CAPABILITIES",
    ("RouteSnapshotClaims", "routes"): "self.routes.len()>MAX_ROUTES",
    ("RevocationSnapshotClaims", "revoked_grant_ids"): (
        "self.revoked_grant_ids.len()>MAX_REVOCATIONS_PER_CLASS"
    ),
    ("RevocationSnapshotClaims", "revoked_ticket_ids"): (
        "self.revoked_ticket_ids.len()>MAX_REVOCATIONS_PER_CLASS"
    ),
    ("RevocationSnapshotClaims", "revoked_deployment_ids"): (
        "self.revoked_deployment_ids.len()>MAX_REVOCATIONS_PER_CLASS"
    ),
    ("RevocationSnapshotClaims", "revoked_bundle_digests"): (
        "self.revoked_bundle_digests.len()>MAX_REVOCATIONS_PER_CLASS"
    ),
    ("WorkerCommand", "inputs"): "inputs.len()>maximum_inputs",
    ("WorkloadEnvelope", "inputs"): "self.inputs.len()>4_096",
    ("WorkloadEnvelope", "expected_output_digests"): ("self.expected_output_digests.len()>4_096"),
}


def test_every_collection_the_protocol_accepts_is_length_bounded(rust):
    """CLAUDE.md requires bounded queues, parsers, and buffers.

    A peer controls the length of every repeated wire field and of the signature
    bytes, so each one has to meet a declared bound before it reaches an
    allocation or a signing loop.
    """
    structs, enums = rust
    sources = _compact("\n".join(path.read_text() for path in sorted((CRATE / "src").glob("*.rs"))))
    collections: set[tuple[str, str]] = set()
    owners = [rust_name for rust_name, _ in PROJECTION.values()] + ["WorkloadEnvelope"]
    for owner in owners:
        collections.update(
            (owner, field)
            for field, declared in structs[owner].items()
            if declared.startswith(("Vec<", "BTreeSet<"))
        )
    for variant in enums["WorkerCommand"].values():
        collections.update(
            ("WorkerCommand", field)
            for field, declared in variant.items()
            if declared.startswith(("Vec<", "BTreeSet<"))
        )
    assert collections == set(COLLECTION_BOUNDS), (
        "peer-controlled collections changed; declare the bound for each: "
        f"{sorted(collections ^ set(COLLECTION_BOUNDS))}"
    )
    for (owner, field), bound in sorted(COLLECTION_BOUNDS.items()):
        assert bound in sources, f"{owner}.{field} no longer meets its declared bound `{bound}`"


def test_bulk_payloads_travel_by_descriptor_not_by_embedded_bytes(runtime_proto):
    """Only the signature may be `bytes`; scientific payloads move by descriptor.

    An embedded payload would put an unbounded, unverifiable blob inside a
    control message that the node has to buffer before it can authenticate it.
    """
    allowed = {("DetachedSignature", "value"), ("RuntimeDispatchRequest", "request_key")}
    for message, fields in runtime_proto.items():
        for field, spec in fields.items():
            if spec["type"] == "bytes":
                assert (message, field) in allowed, (
                    f"{message}.{field} embeds bytes in a runtime control message"
                )


# ---------------------------------------------------------------------------------------
# Attempt state machine: Rust owns the table, Go mirrors it
# ---------------------------------------------------------------------------------------
# `libs/rust/worker_runtime/src/machine.rs` is the authoritative transition table and
# `control/orchestration/state_machine.go` is a hand-written mirror of it. Neither language's
# own tests can notice the two drifting: Go asserts its table edge by edge against a copy of
# itself, and Rust does the same. A transition added on one side and not the other produces a
# control plane that rejects a status its worker legitimately sent, or accepts one it never
# should have -- and the first symptom is a stuck run in production, not a red build.
#
# Both sides are parsed rather than restated here. A test that hard-coded the expected edges
# would be a third copy to drift.

GO_STATE_MACHINE = ROOT / "control/orchestration/state_machine.go"
RUST_MACHINE = ROOT / "libs/rust/worker_runtime/src/machine.rs"

# Rust spells the states in CamelCase; Go's AttemptState constants carry lowercase wire values.
_RUST_TO_WIRE = {
    "Created": "created",
    "Starting": "starting",
    "Ready": "ready",
    "Leased": "leased",
    "Running": "running",
    "Draining": "draining",
    "Committing": "committing",
    "Completed": "completed",
    "Recovering": "recovering",
    "Cancelling": "cancelling",
    "Cancelled": "cancelled",
    "Failed": "failed",
}

# `(A, B | C)` and `(A | B, C)` both appear in the Rust `matches!` arm, so each side of a tuple
# is a set and the arm denotes their cartesian product.
_RUST_ARM = re.compile(r"\(\s*([A-Za-z|\s]+?),\s*([A-Za-z|\s]+?)\s*\)")
_GO_ROW = re.compile(r"Attempt(\w+):\s*\{([^}]*)\}")
_GO_TARGET = re.compile(r"Attempt(\w+):\s*true")


def _rust_transitions() -> set[tuple[str, str]]:
    text = RUST_MACHINE.read_text(encoding="utf-8")
    start = text.index("matches!(")
    body = text[start : text.index("\n}", start)]
    # Drop the `use` line's brace list, which would otherwise parse as an arm.
    body = body[body.index("(from, to)") :]
    edges: set[tuple[str, str]] = set()
    for sources, targets in _RUST_ARM.findall(body):
        froms = [part.strip() for part in sources.split("|") if part.strip()]
        tos = [part.strip() for part in targets.split("|") if part.strip()]
        if not all(name in _RUST_TO_WIRE for name in froms + tos):
            continue
        for source in froms:
            for target in tos:
                edges.add((_RUST_TO_WIRE[source], _RUST_TO_WIRE[target]))
    return edges


def _go_transitions() -> set[tuple[str, str]]:
    text = GO_STATE_MACHINE.read_text(encoding="utf-8")
    start = text.index("var attemptTransitions")
    body = text[start : text.index("\n}", start)]
    edges: set[tuple[str, str]] = set()
    for source, targets in _GO_ROW.findall(body):
        for target in _GO_TARGET.findall(targets):
            edges.add((source.lower(), target.lower()))
    return edges


def test_rust_attempt_transition_table_is_parseable() -> None:
    """A parse that silently found nothing would make the comparison below vacuous."""
    edges = _rust_transitions()
    assert len(edges) >= 20, f"parsed only {len(edges)} Rust transitions; the parser is broken"
    assert ("cancelling", "cancelled") in edges
    assert ("created", "starting") in edges


def test_go_attempt_transition_table_is_parseable() -> None:
    edges = _go_transitions()
    assert len(edges) >= 20, f"parsed only {len(edges)} Go transitions; the parser is broken"
    assert ("cancelling", "cancelled") in edges


def test_attempt_transition_tables_agree_across_languages() -> None:
    """The Go mirror and the Rust original must denote the same edge set.

    A Go-only edge lets the control plane advance an attempt the worker will never report
    reaching. A Rust-only edge makes the control plane reject a legitimate status, stalling the
    stage until an operator intervenes.
    """
    rust = _rust_transitions()
    go = _go_transitions()
    assert go - rust == set(), f"Go permits transitions Rust does not: {sorted(go - rust)}"
    assert rust - go == set(), f"Rust permits transitions Go does not: {sorted(rust - go)}"


def test_attempt_states_match_the_worker_state_enum() -> None:
    """Both tables must range over exactly the protocol's WorkerState values."""
    declared = proto_enums(RUNTIME_V1).get("WorkerState", [])
    wire = {
        value.removeprefix("WORKER_STATE_").lower()
        for value in declared
        if value != "WORKER_STATE_UNSPECIFIED"
    }
    assert wire, "WorkerState enum was not found in the runtime protocol"
    reachable = {state for edge in _rust_transitions() for state in edge}
    assert reachable <= wire, f"the transition table names states the protocol does not: {sorted(reachable - wire)}"
    assert set(_RUST_TO_WIRE.values()) == wire, (
        "the Rust-to-wire state mapping in this test has drifted from the protocol enum: "
        f"{sorted(set(_RUST_TO_WIRE.values()) ^ wire)}"
    )
