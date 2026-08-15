# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import importlib.util
import sys
from pathlib import Path

MODULE = Path(__file__).resolve().parents[1] / "affected.py"
spec = importlib.util.spec_from_file_location("affected", MODULE)
# None means the path is not importable — a non-Python extension or a directory, which is how a
# drifting parents[1] goes wrong. See the fuller note in ci/presubmit/pipeline.py. Guarded before
# the first dereference and raised rather than asserted; at module scope this runs during
# collection, so this message is the only thing pytest will show.
if spec is None or spec.loader is None:
    raise RuntimeError(f"unable to load {MODULE}")
affected = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = affected
spec.loader.exec_module(affected)


def test_global_changes_force_full_graph():
    assert affected.select(["Cargo.toml"]) == ["//..."]


def test_unknown_unowned_file_falls_back_full():
    assert affected.select(["definitely-not-a-package.txt"]) == ["//..."]


def test_rust_qualification_is_affected():
    assert affected.rust_qualification_required(["libs/rust/runtime_core/src/lib.rs"])
    assert affected.rust_qualification_required(["Cargo.toml"])
    assert not affected.rust_qualification_required(["control/artifacts/gc.go"])
