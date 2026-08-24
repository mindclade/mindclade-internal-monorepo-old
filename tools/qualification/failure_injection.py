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
    "control_plane_database_loss",
    "control_plane_transaction_rollback",
    "control_plane_lease_loss",
    "control_plane_duplicate_event",
    "control_plane_retry_exhaustion",
    # Added with the orchestration/scheduling durability work. Listed here, and not only in
    # the TOML, for the same reason every other control-plane scenario is: a scenario that
    # exists only in the matrix file can be deleted from it and this tool still reports
    # "failure-injection matrix passed". The qualification documents in docs/qualification/go
    # cite these four by name, so the claim and the requirement have to move together.
    "control_plane_placement_rollback",
    "control_plane_scheduling_projection_drift",
    "control_plane_scheduling_stale_snapshot",
    "control_plane_scheduling_expiry_backlog",
}


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--execute", action="store_true")
    p.add_argument(
        "--owner",
        action="append",
        default=[],
        help="execute only scenarios owned by this team; all scenarios are still validated",
    )
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
        selected = [s for s in scenarios if not a.owner or s.get("owner") in a.owner]
        if not selected:
            print(f"no failure-injection scenarios matched owner filter: {a.owner}")
            return 1
        for scenario in selected:
            command = list(scenario["command"])
            if shutil.which(command[0]) is None:
                print(f"required tool unavailable for {scenario['name']}: {command[0]}")
                return 1
            listed = subprocess.run(
                [*command, "--list"],
                cwd=ROOT,
                check=True,
                capture_output=True,
                text=True,
            )
            matches = [
                line for line in listed.stdout.splitlines() if line.rstrip().endswith(": test")
            ]
            if len(matches) != 1:
                print(
                    f"{scenario['name']}: expected exactly one matching test, found {len(matches)}"
                )
                return 1
            print(f"failure-injection: {scenario['name']} -> {' '.join(command)}")
            # Reported, not raised. A runner now refuses to count a scenario that skipped as
            # passed, so a developer without a live database reaches this path routinely, and a
            # CalledProcessError traceback buries the runner's own explanation of why under a
            # Python stack that explains nothing.
            if subprocess.run(command, cwd=ROOT, check=False).returncode != 0:
                print(f"failure-injection scenario failed: {scenario['name']}")
                return 1
    print(f"failure-injection matrix passed ({len(scenarios)} scenarios)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
