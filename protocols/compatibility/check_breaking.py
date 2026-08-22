#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Run Buf breaking with one exact, expiring scaffold-removal waiver."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

DELETED_FILE = re.compile(r'^Previously present file "(?P<path>[^"]+)" was deleted\.$')
GO_PACKAGE_CHANGED = re.compile(
    r'^File option "go_package" changed from "" to "(?P<value>[^"]+)"\.$'
)


def _diagnostic(normalized: str) -> tuple[str, str]:
    match = re.match(r"^(?P<path>.*?):[0-9]+:[0-9]+:(?P<message>.*)$", normalized)
    if match is None:
        return "", normalized
    path = match.group("path")
    if path.startswith("protocols/"):
        path = path.removeprefix("protocols/")
    return path, match.group("message")


def _canonical_go_package(path: str) -> str | None:
    parts = path.split("/")
    if len(parts) != 4 or parts[0] != "mindclade" or parts[2] != "v1":
        return None
    package = parts[1]
    return f"go.mindclade.dev/protocols/gen/go/mindclade/{package}/v1;{package}v1"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--against", required=True)
    parser.add_argument("--against-config", required=True)
    parser.add_argument("--buf", default="buf")
    args = parser.parse_args()

    waiver_path = ROOT / "protocols/compatibility/scaffold-removal-waiver.json"
    governance_path = ROOT / "protocols/compatibility/protobuf-surfaces.json"
    waiver = json.loads(waiver_path.read_text(encoding="utf-8"))
    governance = json.loads(governance_path.read_text(encoding="utf-8"))
    expires = dt.date.fromisoformat(waiver["expires_on"])
    if dt.datetime.now(dt.UTC).date() > expires:
        print(f"ERROR: protobuf breaking waiver expired on {expires.isoformat()}")
        return 1

    command = [
        args.buf,
        "breaking",
        "protocols",
        "--against",
        args.against,
        "--against-config",
        args.against_config,
    ]
    completed = subprocess.run(
        command,
        cwd=ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    output = "\n".join(
        part.strip() for part in (completed.stdout, completed.stderr) if part.strip()
    )
    if completed.returncode == 0:
        print(
            "ERROR: scaffold-removal waiver is stale because Buf reports no breaking changes; "
            "delete scaffold-removal-waiver.json and use direct Buf breaking"
        )
        return 1

    allowed_files = set(waiver["allowed_files"])
    baseline = json.loads(
        (ROOT / "protocols/compatibility/protobuf-v1-descriptor.json").read_text(encoding="utf-8")
    )
    baseline_files = {
        path for package in baseline["packages"].values() for path in package["files"]
    }
    tombstone_names = {
        value.rsplit(".", 1)[-1] for value in governance["removed_symbol_tombstones"]
    }
    token = waiver["required_diagnostic_token"]
    rejected: list[str] = []
    accepted = 0
    for line in output.splitlines():
        normalized = line.strip()
        if not normalized:
            continue
        path, message = _diagnostic(normalized)
        deleted = DELETED_FILE.fullmatch(message)
        if deleted is not None:
            deleted_path = f"proto/{deleted.group('path')}"
            if path == "<input>" and deleted_path in allowed_files:
                accepted += 1
                continue
        allowed_symbol = token in message and any(name in message for name in tombstone_names)
        if path in allowed_files and allowed_symbol:
            accepted += 1
            continue
        go_package = GO_PACKAGE_CHANGED.fullmatch(message)
        if go_package is not None and waiver["allow_empty_to_canonical_go_package"]:
            relative = path.removeprefix("proto/")
            expected = _canonical_go_package(relative)
            if relative in baseline_files and go_package.group("value") == expected:
                accepted += 1
                continue
        rejected.append(normalized)
    if rejected or accepted == 0:
        for line in rejected or ["Buf produced no recognized scaffold-removal diagnostics"]:
            print(f"ERROR: unwaived protobuf break: {line}")
        return 1
    print(
        f"accepted {accepted} exact pre-release scaffold-removal diagnostics; "
        f"waiver expires {expires.isoformat()}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
