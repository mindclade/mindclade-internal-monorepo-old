# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Every projection of `mindclade.orchestration.v1.WorkloadEnvelope` is a declared projection.

Four languages each hand-wrote a type called `WorkloadEnvelope` and no build step compared any
of them with the wire message or with each other. They did not merely drift in spelling: Rust
declared `inputs: Vec<BufferDescriptor>` and `expected_output_digests: Vec<Digest>` where the
wire declares `repeated ArtifactRef inputs` and `repeated ArtifactRef expected_outputs`. Those
are different *concepts* under the same field name -- an `ArtifactRef` is content identity
(ADR-0004), a `BufferDescriptor` is leased local placement -- so a decoder written against the
wire would have had to invent the difference, and `services/node_agent` called the divergent
type "the canonical workload envelope" in its own doc comment.

ADR-0026 settles it: `WorkloadEnvelope` names exactly one message. A language may *narrow* the
projection -- drop or delegate a field it must not act on -- but it may never contradict one: a
field it keeps carries the wire's name and a compatible type. Materialized buffers are a
separate node-local concept and travel beside the envelope, which is exactly how the wire
already models them in `mindclade.runtime.v1.RuntimeExecuteRequest`.

Each language below declares `kept`, `renamed`, `delegated`, and `dropped`. The four sets must
partition the wire field set exactly, so a new proto field fails in every language until
somebody classifies it, and a host field nobody claims fails too. `delegated` is a checked
claim, not a hand-wave: the named sub-object must really carry the field.
"""

from __future__ import annotations

import ast
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
PROTO = ROOT / "protocols/proto/mindclade/orchestration/v1/workload.proto"
GO = ROOT / "control/orchestration/workload.go"
RUST = ROOT / "libs/rust/worker_protocol/src/workload.rs"
PYTHON = ROOT / "libs/python/worker_runtime/workload.py"
PYTHON_STAGE = ROOT / "libs/python/worker_runtime/contracts.py"

_PROTO_MESSAGE = re.compile(r"message\s+WorkloadEnvelope\s*\{(.*?)\n\}", re.DOTALL)
_PROTO_FIELD = re.compile(r"^\s*(?:(repeated)\s+)?([\w.]+)\s+(\w+)\s*=\s*(\d+)\s*;")
_GO_STRUCT = re.compile(r"type\s+WorkloadEnvelope\s+struct\s*\{(.*?)\n\}", re.DOTALL)
_RUST_STRUCT = re.compile(r"pub struct WorkloadEnvelope\s*\{(.*?)\n\}", re.DOTALL)
_RUST_FIELD = re.compile(r"^\s*pub\s+(\w+):\s*(.+?),\s*$")


def _snake(name: str) -> str:
    return re.sub(r"(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])", "_", name).lower()


def wire_fields() -> dict[str, str]:
    """Wire field name -> declared type, with `repeated ` kept on the front."""

    text = re.sub(r"//.*", "", PROTO.read_text())
    body = _PROTO_MESSAGE.search(text)
    assert body, f"{PROTO} no longer declares message WorkloadEnvelope"
    fields = {}
    for line in body.group(1).splitlines():
        match = _PROTO_FIELD.match(line)
        if match:
            label, declared, name, _ = match.groups()
            fields[name] = f"repeated {declared}" if label else declared
    assert len(fields) >= 16, f"parsed only {len(fields)} WorkloadEnvelope fields; parser is stale"
    return fields


def go_fields() -> dict[str, str]:
    body = _GO_STRUCT.search(GO.read_text())
    assert body, f"{GO} no longer declares `type WorkloadEnvelope struct`"
    fields: dict[str, str] = {}
    for line in body.group(1).splitlines():
        stripped = re.sub(r"//.*", "", line).strip()
        if not stripped:
            continue
        # Go groups same-typed fields: `WorkloadID, RunID, JobID, StageID string`.
        names, _, declared = stripped.rpartition(" ")
        assert names, f"unparsed Go field line: {stripped!r}"
        for name in names.split(","):
            fields[_snake(name.strip())] = declared.strip()
    return fields


def rust_fields() -> dict[str, str]:
    body = _RUST_STRUCT.search(RUST.read_text())
    assert body, f"{RUST} no longer declares `pub struct WorkloadEnvelope`"
    fields = {}
    for line in body.group(1).splitlines():
        match = _RUST_FIELD.match(re.sub(r"//.*", "", line))
        if match:
            fields[match.group(1)] = match.group(2).strip()
    return fields


def _python_annotations(path: Path, class_name: str) -> dict[str, str]:
    for node in ast.walk(ast.parse(path.read_text(), filename=str(path))):
        if isinstance(node, ast.ClassDef) and node.name == class_name:
            return {
                statement.target.id: ast.unparse(statement.annotation)
                for statement in node.body
                if isinstance(statement, ast.AnnAssign) and isinstance(statement.target, ast.Name)
            }
    raise AssertionError(f"{path} no longer declares class {class_name}")


def python_fields() -> dict[str, str]:
    return _python_annotations(PYTHON, "WorkloadEnvelope")


# --------------------------------------------------------------------------- #
# the reviewed projections (ADR-0026)
# --------------------------------------------------------------------------- #

IDENTIFIERS = ("workload_id", "run_id", "job_id", "stage_id", "tenant_id", "workspace_id")

GO_PROJECTION = {
    "kept": {
        **{name: {"string"} for name in IDENTIFIERS},
        "attempt": {"uint32"},
        "execution_ticket": {"runtime_authority.ExecutionTicket"},
        "inputs": {"[]artifacts.Ref"},
        "expected_outputs": {"[]artifacts.Ref"},
        "resolved_config_digest": {"identifiers.Digest"},
        "resource_class": {"string"},
        "stage_kind": {"StageKind"},
        "operation": {"string"},
    },
    # Go carries the instant, not the encoding. The millisecond form is produced at the wire
    # boundary; a `time.Time` cannot silently disagree about the unit the way a bare integer can.
    "renamed": {"created_unix_millis": "created_at", "deadline_unix_millis": "deadline"},
    "delegated": {},
    "dropped": {},
}

RUST_PROJECTION = {
    "kept": {
        **{name: {"ResourceId"} for name in IDENTIFIERS},
        "attempt": {"u32"},
        "execution_ticket": {"ExecutionTicket"},
        "inputs": {"Vec<ArtifactRef>"},
        "expected_outputs": {"Vec<ArtifactRef>"},
        "resolved_config_digest": {"Digest"},
        "resource_class": {"String"},
        "created_unix_millis": {"u64"},
        "deadline_unix_millis": {"u64"},
        # The Rust enum keeps the name `WorkloadKind`; only the *field* is the wire's. The enum
        # rename is pinned separately by test_worker_protocol.py's stage-taxonomy check.
        "stage_kind": {"WorkloadKind"},
        "operation": {"String"},
    },
    "renamed": {},
    "delegated": {},
    "dropped": {},
}

PYTHON_PROJECTION = {
    "kept": {
        "workload_id": {"str"},
        "run_id": {"str"},
        "job_id": {"str"},
        "tenant_id": {"str"},
        "workspace_id": {"str"},
        "resource_class": {"str"},
        "created_unix_millis": {"int"},
    },
    # The Python worker never verifies a signature -- `libs/python/worker_runtime/contracts.py`
    # states that Rust has already verified signed execution authority before an engine runs.
    # Carrying the signed ticket into the Python process would hand an engine a credential it
    # has no business holding, so the projection narrows to the ticket's identity.
    "renamed": {"execution_ticket": "execution_ticket_id"},
    # StageEnvelope is Python's projection of `mindclade.orchestration.v1.StageSpec`, which
    # carries these same fields. Nesting them mirrors `StageAttempt.spec` on the wire.
    "delegated": {
        "stage_id": ("stage", "stage_id"),
        "attempt": ("stage", "attempt"),
        "stage_kind": ("stage", "kind"),
        "operation": ("stage", "operation"),
        "inputs": ("stage", "inputs"),
        "resolved_config_digest": ("stage", "resolved_config_digest"),
        "deadline_unix_millis": ("stage", "deadline_unix_millis"),
    },
    # The worker writes into the output *namespace* its stage declares and reports what it
    # produced in StageResult. The envelope's expected-output list is control-plane planning
    # state; a worker that read it would be treating a plan as authority over its own results.
    "dropped": {"expected_outputs"},
}

PROJECTIONS = {
    "go": (GO_PROJECTION, go_fields),
    "rust": (RUST_PROJECTION, rust_fields),
    "python": (PYTHON_PROJECTION, python_fields),
}


@pytest.mark.parametrize("language", sorted(PROJECTIONS))
def test_projection_classifies_every_wire_field(language: str) -> None:
    projection, _ = PROJECTIONS[language]
    wire = set(wire_fields())
    classified = (
        set(projection["kept"])
        | set(projection["renamed"])
        | set(projection["delegated"])
        | set(projection["dropped"])
    )
    assert classified == wire, (
        f"{language}: the declared projection does not partition the wire message: "
        f"unclassified={sorted(wire - classified)} not-on-the-wire={sorted(classified - wire)}"
    )


@pytest.mark.parametrize("language", sorted(PROJECTIONS))
def test_projection_carries_the_wire_field_names(language: str) -> None:
    projection, reader = PROJECTIONS[language]
    host = reader()
    expected = set(projection["kept"]) | set(projection["renamed"].values())
    missing = sorted(expected - set(host))
    assert not missing, (
        f"{language}: WorkloadEnvelope is missing fields its projection declares: {missing}. "
        f"present={sorted(host)}"
    )
    # Delegated and dropped fields must be *absent* at the top level, otherwise the projection
    # claims to narrow while actually keeping a second, unchecked copy of the field.
    delegated_or_dropped = set(projection["delegated"]) | set(projection["dropped"])
    kept_anyway = sorted(delegated_or_dropped & set(host))
    assert not kept_anyway, (
        f"{language}: these are declared delegated/dropped but are still top-level fields: "
        f"{kept_anyway}"
    )


@pytest.mark.parametrize("language", sorted(PROJECTIONS))
def test_no_undeclared_field_rides_along(language: str) -> None:
    projection, reader = PROJECTIONS[language]
    claimed = set(projection["kept"]) | set(projection["renamed"].values())
    if language == "python":
        claimed |= {sub for sub, _ in projection["delegated"].values()}
    extra = sorted(set(reader()) - claimed)
    assert not extra, (
        f"{language}: WorkloadEnvelope carries fields the wire does not declare and the "
        f"projection does not claim: {extra}"
    )


@pytest.mark.parametrize("language", ["go", "rust"])
def test_kept_fields_have_a_compatible_host_type(language: str) -> None:
    projection, reader = PROJECTIONS[language]
    host = reader()
    conflicts = [
        f"{field}: declared {host[field]!r}, accepted {sorted(accepted)}"
        for field, accepted in sorted(projection["kept"].items())
        if field in host and host[field] not in accepted
    ]
    assert not conflicts, (
        f"{language}: WorkloadEnvelope contradicts the wire contract -- a field with the wire's "
        f"name must carry the wire's concept: {conflicts}"
    )


def test_python_delegation_targets_really_carry_the_field() -> None:
    stage = _python_annotations(PYTHON_STAGE, "StageEnvelope")
    top = python_fields()
    missing = []
    for wire_name, (holder, sub_field) in sorted(PYTHON_PROJECTION["delegated"].items()):
        if holder not in top:
            missing.append(f"{wire_name}: no `{holder}` field to delegate to")
        elif sub_field not in stage:
            missing.append(f"{wire_name}: {holder}.{sub_field} does not exist")
    assert not missing, (
        f"Python delegates wire fields to something that does not carry them: {missing}"
    )


def test_materialized_buffers_are_not_the_envelopes_inputs() -> None:
    """The node's leased buffers are a separate concept and must not wear `inputs`' name.

    This is the exact defect ADR-0026 settles. `BufferDescriptor` carries a segment, a lease
    expiry and a transport -- placement that is meaningless to the control plane -- while the
    envelope's `inputs` are content identity that outlives any lease. One name for both made
    the two indistinguishable at the seam where they have to be told apart.
    """

    fields = rust_fields()
    for name in ("inputs", "expected_outputs"):
        declared = fields.get(name, "<absent>")
        assert "BufferDescriptor" not in declared and declared != "Vec<Digest>", (
            f"Rust WorkloadEnvelope.{name} is {declared!r}; the wire declares "
            f"{wire_fields().get(name)!r}. Materialized buffers travel beside the envelope, "
            "not inside it."
        )
