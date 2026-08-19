#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce the shape of Go control-plane command packages.

Replaces tests/integration/go_foundation/consumption_test.py, which asserted
that every command wires bootstrap.UnconfiguredFactory -- true only while no
role had a provider factory, so it turned materializing one into a test
failure. It also re-derived the consumption matrix by grepping package-name
strings out of consumption.go; check_foundation_consumption.py now derives that
from the import graph instead.

What survives here is the part that is still an invariant: a command owns no
lifecycle of its own.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

COMMAND_ROOT = "services/control_plane/cmd"
PROFILE_PATH = "services/control_plane/internal/bootstrap/profile.go"
ROLE_CONST_RE = re.compile(r'(?m)^\s*(?:const\s+)?(Role[A-Za-z]+)\s+Role\s*=\s*"([a-z0-9-]+)"')

FORBIDDEN = {
    "servicekit.New(": "command bypasses servicekit/production bootstrap",
    "servicekit.NewAssembly(": "command assembles its own service",
    "signal.Notify": "command takes signal ownership from servicekit",
}


def check(root: Path) -> list[str]:
    command_root = root / COMMAND_ROOT
    if not command_root.is_dir():
        return [f"{COMMAND_ROOT}: missing command root"]

    roles = dict(ROLE_CONST_RE.findall((root / PROFILE_PATH).read_text(encoding="utf-8")))
    by_value = {value: name for name, value in roles.items()}

    errors: list[str] = []
    for directory in sorted(path for path in command_root.iterdir() if path.is_dir()):
        command = directory.name
        role = by_value.get(command.replace("_", "-"))
        if role is None:
            errors.append(f"{COMMAND_ROOT}/{command}: no Role constant declares this command")
            continue

        main = directory / "main.go"
        if not main.is_file():
            errors.append(f"{COMMAND_ROOT}/{command}: missing main.go")
            continue
        source = main.read_text(encoding="utf-8")
        for token in (
            '"go.mindclade.dev/services/control_plane/internal/bootstrap"',
            "bootstrap.Main(",
            f"bootstrap.{role}",
        ):
            if token not in source:
                errors.append(f"{COMMAND_ROOT}/{command}/main.go: must contain {token}")
        for token, message in FORBIDDEN.items():
            if token in source:
                errors.append(f"{COMMAND_ROOT}/{command}/main.go: {message}")

        build = directory / "BUILD.bazel"
        if not build.is_file():
            errors.append(f"{COMMAND_ROOT}/{command}: missing BUILD.bazel")
            continue
        build_text = build.read_text(encoding="utf-8")
        for token in (
            'load("@rules_go//go:def.bzl", "go_binary")',
            "//services/control_plane/internal/bootstrap",
        ):
            if token not in build_text:
                errors.append(f"{COMMAND_ROOT}/{command}/BUILD.bazel: must contain {token}")
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    errors = check(args.repo.resolve())
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f"control-plane command check failed with {len(errors)} finding(s)", file=sys.stderr)
        return 1
    print("control-plane command check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
