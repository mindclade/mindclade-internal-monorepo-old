#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
import os
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
gateway_url = os.environ.get("MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_URL")
gateway_request = os.environ.get("MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_REQUEST")
gateway_pid = os.environ.get("MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_PID")
hardware_command = os.environ.get("MINDCLADE_RUST_HARDWARE_BENCHMARK_COMMAND")
if not gateway_url or not gateway_request or not gateway_pid or not hardware_command:
    print(
        "release performance requires MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_URL, "
        "MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_REQUEST, "
        "MINDCLADE_RUNTIME_GATEWAY_BENCHMARK_PID, and "
        "MINDCLADE_RUST_HARDWARE_BENCHMARK_COMMAND",
        file=sys.stderr,
    )
    raise SystemExit(1)

with tempfile.TemporaryDirectory(prefix="mindclade-performance-") as temporary:
    gateway_results = Path(temporary) / "gateway.json"
    hardware_results = Path(temporary) / "hardware.json"
    gateway_command = [
        sys.executable,
        str(ROOT / "tools/qualification/rust/runtime_gateway_benchmark.py"),
        "--url",
        gateway_url,
        "--request",
        gateway_request,
        "--output",
        str(gateway_results),
    ]
    gateway_command.extend(["--pid", gateway_pid])
    subprocess.run(gateway_command, cwd=ROOT, check=True)

    measured = subprocess.run(
        shlex.split(hardware_command),
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    try:
        hardware = json.loads(measured.stdout)
    except json.JSONDecodeError as error:
        print(f"hardware benchmark did not emit a JSON object: {error}", file=sys.stderr)
        raise SystemExit(1) from error
    if not isinstance(hardware, dict):
        print("hardware benchmark did not emit a JSON object", file=sys.stderr)
        raise SystemExit(1)
    hardware_results.write_text(json.dumps(hardware, sort_keys=True, indent=2) + "\n")

    cmd = [
        sys.executable,
        str(ROOT / "tools/qualification/rust/performance.py"),
        "--measure",
        "--results",
        str(gateway_results),
        "--results",
        str(hardware_results),
        "--require-complete",
    ]
    raise SystemExit(subprocess.call(cmd, cwd=ROOT))
