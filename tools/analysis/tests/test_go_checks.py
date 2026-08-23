# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import importlib.util
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    # None means the path is not importable — a non-Python extension or a directory, which is
    # how a drifting ROOT (parents[3]) goes wrong. See the fuller note in ci/presubmit/pipeline.py.
    # Guarded before the first dereference, and raised rather than asserted: `python -O` drops
    # asserts, and this runs at import time where the message is all anyone gets.
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


layers = load("check_go_layers", ROOT / "tools/analysis/check_go_layers.py")
placeholders = load(
    "check_placeholder_packages", ROOT / "tools/analysis/check_placeholder_packages.py"
)


def test_layer_classifier_distinguishes_contracts_and_adapters() -> None:
    assert layers.layer_for("faults") == 0
    assert layers.layer_for("messaging") == 1
    assert layers.layer_for("servicekit/production") == 2
    assert layers.layer_for("coordination/outbox/postgres") == 3
    assert layers.layer_for("httpx/outbound") == 4


def test_higher_layer_import_is_rejected() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        path = root / "libs/go/faults/bad.go"
        path.parent.mkdir(parents=True)
        path.write_text('package faults\nimport "go.mindclade.dev/libs/go/servicekit"\n')
        violations = layers.import_violations(root, [path])
        assert violations and "higher layer" in violations[0].message


def _write(root: Path, relative: str, body: str) -> Path:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)
    return path


def test_unclassifiable_libs_go_package_fails_closed() -> None:
    """The regression: an unrecognized package used to disable the gate silently.

    `layer_for` returns None for any package it does not recognize, and the import rule
    used to skip the comparison whenever either side was None. A newly admitted
    `libs/go/<name>` therefore had every intra-libs/go edge checked by nothing while the
    checker printed "Go architecture check passed" — the gate stopped applying to exactly
    the code most likely to violate layering. This fixture is that case: `scheduling` is
    outside the recognized set and its only import reaches Layer 4.
    """
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        offender = _write(
            root,
            "libs/go/scheduling/scheduler.go",
            'package scheduling\nimport "go.mindclade.dev/libs/go/httpx"\n',
        )
        _write(root, "libs/go/httpx/httpx.go", "package httpx\n")

        assert layers.layer_for("scheduling") is None
        rendered = [violation.render(root) for violation in layers.check(root)]
        # The package itself is named, so the fix location is unambiguous.
        assert any(
            value.startswith("libs/go/scheduling:") and "has no layer" in value
            for value in rendered
        ), rendered
        # And the specific edge that went unvalidated is named, so the blast radius of a
        # missing classification is visible rather than inferred.
        assert any(
            "scheduler.go" in value and "cannot be layer-checked" in value for value in rendered
        ), rendered
        assert layers.import_violations(root, [offender])


def test_unclassifiable_import_target_is_reported() -> None:
    """The mirror image: a known source importing a package with no layer."""
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        path = _write(
            root,
            "libs/go/faults/faults.go",
            'package faults\nimport "go.mindclade.dev/libs/go/scheduling"\n',
        )
        messages = [violation.message for violation in layers.import_violations(root, [path])]
        assert messages == [
            "import go.mindclade.dev/libs/go/scheduling cannot be layer-checked: "
            "libs/go package scheduling has no layer in check_go_layers.layer_for"
        ]


def test_non_libs_go_imports_are_still_ignored() -> None:
    """Fail-closed must mean "libs/go package I cannot classify", not "unknown import".

    The standard library and third-party modules are not `libs/go` packages and have no
    layer to compare against. Widening the error to every unclassifiable import path would
    have failed on every file in the repository; this pins the distinction.
    """
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        path = _write(
            root,
            "libs/go/faults/faults.go",
            "package faults\n"
            "import (\n"
            '\t"context"\n'
            '\t"github.com/google/uuid"\n'
            '\t"go.mindclade.dev/sdk/go/mindclade"\n'
            '\t"go.mindclade.dev/libs/go/identifiers"\n'
            ")\n",
        )
        assert layers.import_violations(root, [path]) == []


def test_internal_rpcfaults_is_a_transport_adapter() -> None:
    """`internal/rpcfaults` is Layer 4, as LAYERS.md has always said.

    Its doc comment scopes it to the Connect and gRPC adapters and connectx, grpcx, and
    httpx are its only importers, all Layer 4. Classifying it Layer 1 licensed the one
    direction LAYERS.md forbids -- "Lower layers never import Layer 4" -- by letting a
    Layer 1 contract or Layer 2 mechanism import transport fault translation cleanly.
    """
    assert layers.layer_for("internal/rpcfaults") == 4
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        path = _write(
            root,
            "libs/go/requestmeta/meta.go",
            'package requestmeta\nimport "go.mindclade.dev/libs/go/internal/rpcfaults"\n',
        )
        violations = layers.import_violations(root, [path])
        assert violations and "higher layer 4" in violations[0].message


def test_every_repository_libs_go_package_is_classified() -> None:
    """Ratchet: the real tree must stay fully classified, so the gate stays fully applied."""
    unclassified = [
        violation.render(ROOT) for violation in layers.unclassified_package_violations(ROOT)
    ]
    assert unclassified == []


def test_placeholder_checker_targets_promoted_foundation() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        for relative in [
            "libs/go/example",
            "services/control_plane/internal/bootstrap",
            "services/control_plane/internal/foundation",
            "services/control_plane/internal/config",
            "services/control_plane/internal/transport",
        ]:
            (root / relative).mkdir(parents=True)
            (root / relative / "x.go").write_text("package x\n")
        (root / "libs/go/example/x.go").write_text('package example\nconst scaffold_x = "x"\n')
        violations = placeholders.check(root)
        assert any("scaffold_x" in violation for violation in violations)
