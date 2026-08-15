#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Runtime gateway/host/node-agent qualification."""

from common import require_tool, run, verify_toolchain


def main() -> int:
    verify_toolchain()
    cargo = require_tool("cargo")
    for package in (
        "mindclade-serving-runtime",
        "mindclade-runtime-gateway",
        "mindclade-runtime-host",
        "mindclade-node-agent",
    ):
        run([cargo, "test", "-p", package, "--all-targets", "--locked"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
