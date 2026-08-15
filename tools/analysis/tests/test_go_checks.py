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
    # Guard before the first dereference, and raise rather than assert — see the matching
    # comment in ci/presubmit/pipeline.py. This runs at import time, so a bad path here fails
    # collection for the whole module and the message is all anyone gets.
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
        path.write_text('package faults\nimport "mindclade.internal/libs/go/servicekit"\n')
        violations = layers.import_violations(root, [path])
        assert violations and "higher layer" in violations[0].message


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
