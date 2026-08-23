# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load_module():
    path = ROOT / "tools/analysis/check_control_plane_commands.py"
    spec = importlib.util.spec_from_file_location("check_control_plane_commands", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


commands = load_module()
LABEL = "services/control_plane/cmd/api/BUILD.bazel"
BOOTSTRAP = "//services/control_plane/internal/bootstrap"


def test_accepts_gazelle_multi_symbol_load_and_embedded_library() -> None:
    source = f'''
load("@rules_go//go:def.bzl", "go_binary", "go_library")

go_binary(name = "api", embed = [":api_lib"])
go_library(name = "api_lib", deps = ["{BOOTSTRAP}"])
'''
    assert commands.build_contract_errors(source, LABEL) == []


def test_rejects_comment_only_contract_text() -> None:
    source = f'''
# load("@rules_go//go:def.bzl", "go_binary")
# go_binary(name = "api", deps = ["{BOOTSTRAP}"])
exports_files(["main.go"])
'''
    errors = commands.build_contract_errors(source, LABEL)
    assert len(errors) == 3
    assert any("must load go_binary" in error for error in errors)
    assert any("must declare a go_binary" in error for error in errors)
    assert any("must declare dependency" in error for error in errors)


def test_rejects_bootstrap_dependency_outside_rule() -> None:
    source = f'''
load("@rules_go//go:def.bzl", "go_binary")
BOOTSTRAP = "{BOOTSTRAP}"
go_binary(name = "api")
'''
    assert commands.build_contract_errors(source, LABEL) == [
        f"{LABEL}: must declare dependency {BOOTSTRAP}"
    ]
