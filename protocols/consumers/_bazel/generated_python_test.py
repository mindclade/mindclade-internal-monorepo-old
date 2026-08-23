# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Python mirror of ``generated_go_test.go``.

Go gets one importable package per proto package, so the Go conformance test proves the
generated surface by importing ten packages. protoc emits one Python module per ``.proto``
file, so the Python surface is only as complete as the ``py_proto_library`` ``deps`` lists
that reach it: a package whose ``py_proto_library`` names a subset of its ``proto_library``
targets still builds green, and the omission surfaces as an ``ImportError`` in a Python
caller long after the fact. That is the failure this module exists to make loud.

It lives in the same underscore-prefixed Bazel package as the Go test, for the same
reason: generated bindings are Bazel action outputs, never checked in, so nothing outside
a Bazel action can import them. ``testpaths`` in ``pyproject.toml`` does not list
``protocols``, so the repository-wide ``pytest`` run does not try to collect this file and
fail on imports that only exist inside a Bazel runfiles tree.
"""

from __future__ import annotations

import importlib
from pathlib import Path

import pytest

# One representative message per promoted proto package, exactly as the Go test does.
# Registration under the canonical fully-qualified name is what makes the module a
# projection of `protocols/` rather than a coincidentally similar Python class.
from mindclade.artifact.v1 import artifact_pb2
from mindclade.common.v1 import identifiers_pb2
from mindclade.data.v1 import snapshot_pb2
from mindclade.evaluation.v1 import evaluation_pb2
from mindclade.events.v1 import envelope_pb2
from mindclade.inference.v1 import request_pb2
from mindclade.orchestration.v1 import workflow_pb2
from mindclade.registry.v1 import checkpoint_pb2 as registry_checkpoint_pb2
from mindclade.runtime.v1 import execution_ticket_pb2
from mindclade.training.v1 import run_pb2 as training_run_pb2

# `protocols/proto` is the import root protoc is given, so the module path is a mechanical
# transform of the source path. Deriving it here — rather than listing 47 module names —
# is what lets the coverage assertion below notice a `.proto` file that no
# `py_proto_library` reaches.
PROTO_ROOT = Path(__file__).resolve().parents[3] / "protocols/proto/mindclade"

# Packages whose `py_proto_library` coverage is still partial, pinned exactly.
#
# This is a ratchet, not an allowlist. The assertion below is set equality in both
# directions: adding a `.proto` with no Python binding fails, and closing one of these gaps
# without shrinking this set fails just as loudly. Every entry is in `artifact/v1`,
# `registry/v1`, or `runtime/v1` — the three packages that already carried a
# `py_proto_library` covering only the subset one caller happened to need, and whose
# BUILD files are outside the change that introduced this test. Delete an entry in the
# same commit that adds its `proto_library` to the owning package's `py_proto_library`.
UNCOVERED_BY_PY_PROTO_LIBRARY = frozenset(
    {
        "mindclade.artifact.v1.manifest_pb2",
        "mindclade.artifact.v1.service_pb2",
        "mindclade.registry.v1.model_bundle_pb2",
        "mindclade.registry.v1.reference_database_pb2",
        "mindclade.registry.v1.service_pb2",
        "mindclade.runtime.v1.execution_service_pb2",
        "mindclade.runtime.v1.route_snapshot_pb2",
        "mindclade.runtime.v1.service_pb2",
    }
)

CANONICAL_MESSAGE_NAMES = (
    (artifact_pb2.ArtifactRef, "mindclade.artifact.v1.ArtifactRef"),
    (identifiers_pb2.ResourceId, "mindclade.common.v1.ResourceId"),
    (snapshot_pb2.SourceSnapshot, "mindclade.data.v1.SourceSnapshot"),
    (evaluation_pb2.Evaluation, "mindclade.evaluation.v1.Evaluation"),
    (envelope_pb2.EventEnvelope, "mindclade.events.v1.EventEnvelope"),
    (request_pb2.InferenceRequest, "mindclade.inference.v1.InferenceRequest"),
    (workflow_pb2.Workflow, "mindclade.orchestration.v1.Workflow"),
    (registry_checkpoint_pb2.CheckpointRecord, "mindclade.registry.v1.CheckpointRecord"),
    (execution_ticket_pb2.ExecutionTicket, "mindclade.runtime.v1.ExecutionTicket"),
    (training_run_pb2.TrainingRunSpecification, "mindclade.training.v1.TrainingRunSpecification"),
)


def _promoted_modules() -> list[str]:
    """Every promoted `.proto`, as the Python module protoc generates for it."""
    modules = [
        ".".join(source.relative_to(PROTO_ROOT.parent).with_suffix("").parts) + "_pb2"
        for source in sorted(PROTO_ROOT.glob("*/v1/*.proto"))
    ]
    # An empty runfiles tree would otherwise make every assertion below vacuous: the
    # coverage partition of nothing is trivially consistent with itself.
    assert modules, f"no promoted .proto sources found under {PROTO_ROOT}"
    return modules


@pytest.mark.parametrize(("message_type", "expected_name"), CANONICAL_MESSAGE_NAMES)
def test_promoted_package_registers_canonical_message_name(
    message_type: type, expected_name: str
) -> None:
    """Each promoted package reaches Python under its canonical wire identity."""
    assert message_type.DESCRIPTOR.full_name == expected_name


def test_every_promoted_proto_generates_an_importable_python_module() -> None:
    """The generated Python surface covers every promoted `.proto` but the pinned gaps."""
    uncovered = set()
    for module_name in _promoted_modules():
        try:
            importlib.import_module(module_name)
        except ImportError:
            uncovered.add(module_name)

    assert uncovered == set(UNCOVERED_BY_PY_PROTO_LIBRARY), (
        "generated Python coverage moved. Newly missing modules mean a .proto has no "
        "py_proto_library reaching it and Python callers must hand-mirror it; newly "
        "present modules mean UNCOVERED_BY_PY_PROTO_LIBRARY is stale and must shrink. "
        f"missing={sorted(uncovered - UNCOVERED_BY_PY_PROTO_LIBRARY)} "
        f"newly_covered={sorted(UNCOVERED_BY_PY_PROTO_LIBRARY - uncovered)}"
    )


def test_generated_modules_round_trip_through_the_wire() -> None:
    """A linkable module is not yet a usable one; parse what the descriptors serialize.

    The cross-package case is the one worth pinning: `stream.proto` carries a
    `common/v1.ErrorDetail`, so a `py_proto_library` that reached `inference/v1` without
    dragging `common/v1` in would import and then fail on field access.
    """
    from mindclade.common.v1 import errors_pb2
    from mindclade.inference.v1 import stream_pb2

    event = stream_pb2.InferenceStreamEvent()
    event.error.CopyFrom(errors_pb2.ErrorDetail(message="stream aborted"))

    decoded = stream_pb2.InferenceStreamEvent()
    decoded.ParseFromString(event.SerializeToString())
    assert decoded.error.message == "stream aborted"
