#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Deterministically generate and verify the public TypeScript SDK contracts."""

from __future__ import annotations

import argparse
import hashlib
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GENERATED = (
    ROOT / "sdk/typescript/src/generated/api.ts",
    ROOT / "sdk/typescript/src/generated/proto",
)


def digest_path(path: Path) -> str:
    digest = hashlib.sha256()
    if path.is_file():
        digest.update(path.relative_to(ROOT).as_posix().encode())
        digest.update(path.read_bytes())
        return digest.hexdigest()
    if path.is_dir():
        for child in sorted(item for item in path.rglob("*") if item.is_file()):
            digest.update(child.relative_to(ROOT).as_posix().encode())
            digest.update(child.read_bytes())
        return digest.hexdigest()
    return "missing"


def generate() -> None:
    subprocess.run(
        ["pnpm", "exec", "buf", "generate", "protocols", "--template", "protocols/buf.gen.yaml"],
        cwd=ROOT,
        check=True,
    )
    subprocess.run(
        [
            "pnpm",
            "exec",
            "openapi-typescript",
            "protocols/openapi/public.openapi.yaml",
            "-o",
            "sdk/typescript/src/generated/api.ts",
        ],
        cwd=ROOT,
        check=True,
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="fail if regeneration changes output")
    args = parser.parse_args()
    before = {path: digest_path(path) for path in GENERATED}
    generate()
    after = {path: digest_path(path) for path in GENERATED}
    if args.check and before != after:
        changed = [path.relative_to(ROOT).as_posix() for path in GENERATED if before[path] != after[path]]
        print("generated TypeScript SDK drift: " + ", ".join(changed))
        return 1
    print("TypeScript SDK generation is deterministic and current" if args.check else "Generated TypeScript SDK contracts")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
