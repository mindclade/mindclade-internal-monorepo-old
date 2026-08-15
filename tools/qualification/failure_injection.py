#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate and optionally execute the durable-boundary failure matrix."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = {
    "worker_killed_before_artifact_commit",
    "worker_killed_after_upload_before_ack",
    "lease_expires_during_preprocessing",
    "checkpoint_upload_interrupted",
    "object_store_unavailable",
    "control_plane_unavailable_after_admission",
}


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--execute", action="store_true")
    a = p.parse_args()
    data = tomllib.loads((ROOT / "configs/qualification/failure_injection.toml").read_text())
    scenarios = data.get("scenario", [])
    names = {s.get("name") for s in scenarios}
    failures = []
    if missing := REQUIRED - names:
        failures.append(f"missing failure-injection scenarios: {sorted(missing)}")
    for s in scenarios:
        if not s.get("invariant") or not s.get("owner"):
            failures.append(f"{s.get('name')}: missing invariant/owner")
        cmd = s.get("command")
        if (
            not isinstance(cmd, list)
            or not cmd
            or any(not isinstance(x, str) or not x for x in cmd)
        ):
            failures.append(f"{s.get('name')}: invalid command")
    if failures:
        print("\n".join(failures))
        return 1
    if a.execute:
        for scenario in scenarios:
            command = list(scenario["command"])
            if shutil.which(command[0]) is None:
                print(f"required tool unavailable for {scenario['name']}: {command[0]}")
                return 1
            print(f"failure-injection: {scenario['name']} -> {' '.join(command)}")
            subprocess.run(command, cwd=ROOT, check=True)
    print(f"failure-injection matrix passed ({len(scenarios)} scenarios)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
