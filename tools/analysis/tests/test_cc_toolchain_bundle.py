# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import importlib.util
import json
import os
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


validator = load(
    "validate_cc_toolchain_bundle", ROOT / "tools/analysis/validate_cc_toolchain_bundle.py"
)


def bundle(tmp_path: Path, *, system: str = "x86_64-linux") -> Path:
    root = tmp_path / "bundle"
    bin_dir = root / "bin"
    include_dir = root / "include"
    bin_dir.mkdir(parents=True)
    include_dir.mkdir()
    tools = {}
    for name in sorted(validator.REQUIRED_TOOLS):
        tool = bin_dir / name
        tool.write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
        tool.chmod(0o755)
        tools[name] = f"bin/{name}"
    darwin = system.endswith("darwin")
    sdk = root / "sdk"
    link_directory = root / "lib"
    if darwin:
        sdk.mkdir()
        link_directory.mkdir()
    manifest = {
        "schema": 1,
        "system": system,
        "target_cpu": "darwin_arm64" if darwin else "k8",
        "target_triple": "arm64-apple-darwin" if darwin else "x86_64-unknown-linux-gnu",
        "constraints": {
            "cpu": "aarch64" if darwin else "x86_64",
            "os": "osx" if darwin else "linux",
        },
        "compiler": "clang",
        "builtin_include_directories": [str(include_dir)],
        "sdk_root": str(sdk) if darwin else "",
        "darwin_deployment_target": "14.0" if darwin else None,
        "link_library_directories": [str(link_directory)] if darwin else [],
        "tools": tools,
    }
    (root / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    return root


def test_valid_linux_and_darwin_bundles_pass(tmp_path: Path) -> None:
    assert validator.validate(bundle(tmp_path / "linux"))["compiler"] == "clang"
    assert validator.validate(bundle(tmp_path / "darwin", system="aarch64-darwin"))["sdk_root"]


def test_missing_compiler_reports_exact_tool(tmp_path: Path) -> None:
    root = bundle(tmp_path)
    os.unlink(root / "bin/gcc")
    with pytest.raises(validator.BundleError, match="required tool 'gcc' is missing"):
        validator.validate(root)


def test_missing_darwin_sdk_has_focused_diagnostic(tmp_path: Path) -> None:
    root = bundle(tmp_path, system="aarch64-darwin")
    os.rmdir(root / "sdk")
    with pytest.raises(validator.BundleError, match="Apple SDK is missing"):
        validator.validate(root)


def test_missing_darwin_link_library_has_focused_diagnostic(tmp_path: Path) -> None:
    root = bundle(tmp_path, system="aarch64-darwin")
    os.rmdir(root / "lib")
    with pytest.raises(validator.BundleError, match="link library directory is missing"):
        validator.validate(root)
