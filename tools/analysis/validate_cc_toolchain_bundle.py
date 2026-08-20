#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate the on-disk contract consumed by the Nix C/C++ Bzlmod extension."""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

REQUIRED_TOOLS = frozenset(
    {"ar", "cpp", "cxx_linker", "gcc", "ld", "nm", "objcopy", "objdump", "strip"}
)


class BundleError(ValueError):
    """The bundle is absent or malformed."""


def _required_string(value: object, field: str) -> str:
    if not isinstance(value, str) or not value:
        raise BundleError(f"manifest field {field!r} must be a non-empty string")
    return value


def validate(bundle: Path) -> dict[str, object]:
    manifest_path = bundle / "manifest.json"
    if not manifest_path.is_file():
        raise BundleError(f"toolchain manifest is missing: {manifest_path}")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise BundleError(f"toolchain manifest is not valid JSON: {error}") from error
    if not isinstance(manifest, dict) or manifest.get("schema") != 1:
        raise BundleError("toolchain manifest must be a schema-1 JSON object")
    if _required_string(manifest.get("compiler"), "compiler") != "clang":
        raise BundleError("toolchain compiler must be clang")
    for field in ("system", "target_cpu", "target_triple"):
        _required_string(manifest.get(field), field)

    constraints = manifest.get("constraints")
    if not isinstance(constraints, dict):
        raise BundleError("manifest field 'constraints' must be an object")
    constraint_os = _required_string(constraints.get("os"), "constraints.os")
    _required_string(constraints.get("cpu"), "constraints.cpu")

    tools = manifest.get("tools")
    if not isinstance(tools, dict):
        raise BundleError("manifest field 'tools' must be an object")
    missing_names = REQUIRED_TOOLS - tools.keys()
    if missing_names:
        raise BundleError(f"manifest omits required tools: {', '.join(sorted(missing_names))}")
    for name in sorted(REQUIRED_TOOLS):
        relative = _required_string(tools.get(name), f"tools.{name}")
        relative_path = Path(relative)
        if relative_path.is_absolute() or ".." in relative_path.parts:
            raise BundleError(f"required tool {name!r} has an unsafe path: {relative}")
        tool = bundle / relative_path
        if not tool.is_file() or not os.access(tool, os.X_OK):
            raise BundleError(f"required tool {name!r} is missing or not executable: {tool}")

    includes = manifest.get("builtin_include_directories")
    if not isinstance(includes, list) or not includes:
        raise BundleError("manifest requires at least one builtin include directory")
    for include in includes:
        if not isinstance(include, str) or not Path(include).is_dir():
            raise BundleError(f"builtin include directory is missing: {include}")

    link_directories = manifest.get("link_library_directories", [])
    if not isinstance(link_directories, list):
        raise BundleError("manifest field 'link_library_directories' must be an array")
    for directory in link_directories:
        if not isinstance(directory, str) or not Path(directory).is_dir():
            raise BundleError(f"link library directory is missing: {directory}")

    if constraint_os == "osx":
        sdk_root = manifest.get("sdk_root")
        if not isinstance(sdk_root, str) or not Path(sdk_root).is_dir():
            raise BundleError(f"Apple SDK is missing: {sdk_root}")
        deployment = manifest.get("darwin_deployment_target")
        if not isinstance(deployment, str) or not deployment:
            raise BundleError("Darwin toolchain requires darwin_deployment_target")
        if not link_directories:
            raise BundleError("Darwin toolchain requires pinned link library directories")
    return manifest


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "bundle",
        nargs="?",
        type=Path,
        default=os.environ.get("MINDCLADE_CC_TOOLCHAIN_ROOT"),
    )
    args = parser.parse_args(argv)
    if args.bundle is None:
        print(
            "cc toolchain bundle check failed: MINDCLADE_CC_TOOLCHAIN_ROOT is not set",
            file=sys.stderr,
        )
        return 2
    try:
        manifest = validate(args.bundle)
    except BundleError as error:
        print(f"cc toolchain bundle check failed: {error}", file=sys.stderr)
        return 1
    print(
        "cc toolchain bundle check passed: "
        f"{manifest['system']} {manifest['target_triple']} {manifest['compiler']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
