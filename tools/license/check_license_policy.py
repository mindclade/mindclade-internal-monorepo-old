#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Fail closed when first-party license texts or package metadata drift.

The source-header checker answers which files Mindclade authored. This checker answers which
terms those files and first-party packages declare, while pinning independently licensed
material so it cannot be silently overwritten with the proprietary license.
"""

from __future__ import annotations

import hashlib
import json
import subprocess
import sys
import tomllib
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
PROPRIETARY_LICENSE = "LicenseRef-Mindclade-Proprietary"
NODE_PRIVATE_LICENSE = "UNLICENSED"

# These digests are the reviewed estate contract. Updating either value requires updating the
# canonical file in every repository and passing every repository-home validation gate.
CANONICAL_LICENSE_SHA256 = "a3fe40dac91dfc0c71eb8dc8ceb1c7b606ab8bc5fe26e09b8e53f6b9f8c8d57f"
CANONICAL_HEADER_SHA256 = "0f2b024dbf454c08d57b663d8ad8e469215984a7007ef66bd37d651e046e0029"

# Independently licensed material is deliberately not normalized to the Mindclade license.
# Pinning its controlling text makes an accidental relicense or deletion a hard failure.
INDEPENDENT_LICENSE_SHA256 = {
    ".agents/skills/LICENSE": "65d75666be49e69ec9a041d0eb80b7735eae391fd085c7290f6ac06a550f8d18",
}

SKIP_PARTS = frozenset({".agents", "generated", "node_modules", "vendor"})


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tracked_paths(root: Path) -> list[Path]:
    result = subprocess.run(
        ["git", "-C", str(root), "ls-files", "-z"],
        check=True,
        capture_output=True,
        text=True,
    )
    return [root / relative for relative in result.stdout.split("\0") if relative]


def is_first_party_manifest(path: Path, root: Path) -> bool:
    relative = path.relative_to(root)
    return not any(part in SKIP_PARTS for part in relative.parts)


def load_toml(path: Path, errors: list[str]) -> dict[str, Any] | None:
    try:
        with path.open("rb") as handle:
            return tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        errors.append(f"{path.relative_to(REPO_ROOT)}: invalid TOML: {exc}")
        return None


def check_root_contract(root: Path, errors: list[str]) -> None:
    required = {
        "LICENSE": CANONICAL_LICENSE_SHA256,
        ".github/MINDCLADE_PROPRIETARY_SOURCE_HEADER.txt": CANONICAL_HEADER_SHA256,
    }
    for relative, expected in required.items():
        path = root / relative
        if not path.is_file():
            errors.append(f"{relative}: required license contract file is missing")
        elif sha256(path) != expected:
            errors.append(f"{relative}: content differs from the reviewed estate contract")

    notice = root / "NOTICE"
    if not notice.is_file():
        errors.append("NOTICE: required third-party notice is missing")
    else:
        notice_text = notice.read_text(encoding="utf-8")
        if ".agents/skills/LICENSE" not in notice_text:
            errors.append("NOTICE: independently licensed agent skills are not disclosed")

    for relative, expected in INDEPENDENT_LICENSE_SHA256.items():
        path = root / relative
        if not path.is_file():
            errors.append(f"{relative}: independently licensed controlling text is missing")
        elif sha256(path) != expected:
            errors.append(f"{relative}: independently licensed controlling text changed")


def check_node_manifest(path: Path, errors: list[str]) -> None:
    relative = path.relative_to(REPO_ROOT)
    try:
        metadata = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        errors.append(f"{relative}: invalid package.json: {exc}")
        return

    if metadata.get("private") is not True:
        errors.append(f"{relative}: first-party Node package must set private=true")
    if metadata.get("license") != NODE_PRIVATE_LICENSE:
        errors.append(
            f'{relative}: first-party Node package must set license="{NODE_PRIVATE_LICENSE}"'
        )


def cargo_license_is_proprietary(package: dict[str, Any]) -> bool:
    license_value = package.get("license")
    return license_value == PROPRIETARY_LICENSE or license_value == {"workspace": True}


def check_cargo_manifest(path: Path, errors: list[str]) -> None:
    metadata = load_toml(path, errors)
    if metadata is None:
        return
    relative = path.relative_to(REPO_ROOT)

    workspace_package = metadata.get("workspace", {}).get("package")
    if path == REPO_ROOT / "Cargo.toml":
        if not isinstance(workspace_package, dict):
            errors.append("Cargo.toml: [workspace.package] is missing")
        else:
            if workspace_package.get("publish") is not False:
                errors.append("Cargo.toml: workspace.package.publish must be false")
            if workspace_package.get("license") != PROPRIETARY_LICENSE:
                errors.append(
                    f'Cargo.toml: workspace.package.license must be "{PROPRIETARY_LICENSE}"'
                )

    package = metadata.get("package")
    if not isinstance(package, dict):
        return
    if package.get("publish") is not False:
        errors.append(f"{relative}: first-party Rust package must set publish=false")
    if not cargo_license_is_proprietary(package):
        errors.append(f"{relative}: first-party Rust package has no proprietary license")


def check_python_manifest(path: Path, errors: list[str]) -> None:
    metadata = load_toml(path, errors)
    if metadata is None:
        return
    project = metadata.get("project")
    if not isinstance(project, dict):
        return
    relative = path.relative_to(REPO_ROOT)

    if project.get("license") != PROPRIETARY_LICENSE:
        errors.append(f"{relative}: Python project has no proprietary SPDX license expression")

    license_files = project.get("license-files")
    if not isinstance(license_files, list) or not license_files:
        errors.append(f"{relative}: Python project must package its license and notice files")
        return
    for pattern in license_files:
        if not isinstance(pattern, str) or ".." in Path(pattern).parts:
            errors.append(f"{relative}: invalid license-files entry {pattern!r}")
            continue
        if not any(path.parent.glob(pattern)):
            errors.append(f"{relative}: license-files pattern matches nothing: {pattern}")


def check(root: Path = REPO_ROOT) -> list[str]:
    global REPO_ROOT
    root = root.resolve()
    previous_root = REPO_ROOT
    REPO_ROOT = root
    errors: list[str] = []
    try:
        check_root_contract(root, errors)
        for path in tracked_paths(root):
            if not is_first_party_manifest(path, root):
                continue
            if path.name == "package.json":
                check_node_manifest(path, errors)
            elif path.name == "Cargo.toml":
                check_cargo_manifest(path, errors)
            elif path.name == "pyproject.toml":
                check_python_manifest(path, errors)
    finally:
        REPO_ROOT = previous_root
    return errors


def main() -> int:
    errors = check()
    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        print(f"\n{len(errors)} license policy violation(s).", file=sys.stderr)
        return 1
    print("License text, independent-license preservation, and package metadata are aligned.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
