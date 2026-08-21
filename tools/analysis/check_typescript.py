#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed source and workspace checks for the TypeScript product surface."""

from __future__ import annotations

import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SOURCE_ROOTS = (ROOT / "apps", ROOT / "libs" / "ts", ROOT / "sdk" / "typescript")
IGNORED_DIRECTORIES = {".next", "dist", "generated", "node_modules"}
FORBIDDEN = {
    "SCAFFOLD_": "scaffold sentinel",
    "data-scaffold=": "scaffold-only markup",
    "echo scaffold": "fake package command",
    "@ts-ignore": "unchecked compiler suppression",
    "@ts-nocheck": "disabled type checking",
}
REQUIRED_SCRIPTS = {"build", "lint", "test", "typecheck"}
TS_SUFFIXES = {".ts", ".tsx", ".mts", ".cts"}


def main() -> int:
    errors: list[str] = []
    for source_root in SOURCE_ROOTS:
        for path in source_root.rglob("*"):
            if not path.is_file() or IGNORED_DIRECTORIES.intersection(path.parts):
                continue
            if path.suffix in TS_SUFFIXES or path.name in {"package.json", "next.config.ts"}:
                text = path.read_text(encoding="utf-8", errors="replace")
                for needle, description in FORBIDDEN.items():
                    if needle in text:
                        errors.append(f"{path.relative_to(ROOT)}: contains {description}: {needle}")
                if path.suffix in TS_SUFFIXES and re.search(r"\bas\s+any\b|:\s*any\b", text):
                    errors.append(f"{path.relative_to(ROOT)}: uses explicit any")

        for manifest in source_root.glob("*/package.json") if source_root.name != "typescript" else [source_root / "package.json"]:
            if not manifest.exists():
                continue
            data = json.loads(manifest.read_text(encoding="utf-8"))
            missing = sorted(REQUIRED_SCRIPTS - set(data.get("scripts", {})))
            if missing:
                errors.append(f"{manifest.relative_to(ROOT)}: missing scripts: {', '.join(missing)}")

    generated_api = ROOT / "sdk/typescript/src/generated/api.ts"
    generated_proto = ROOT / "sdk/typescript/src/generated/proto"
    if not generated_api.exists():
        errors.append("sdk/typescript/src/generated/api.ts: generated OpenAPI types are missing")
    if not generated_proto.exists() or not any(generated_proto.rglob("*_pb.ts")):
        errors.append("sdk/typescript/src/generated/proto: generated Protobuf-ES bindings are missing")

    for error in errors:
        print(error)
    if errors:
        print(f"TypeScript architecture check failed: {len(errors)} error(s)")
        return 1
    print("TypeScript architecture check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
