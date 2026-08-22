#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Verify one connected training-platform qualification set."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from tools.qualification.training_gke.evidence import (
    load_qualification_set,
    validate_qualification_set,
)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("evidence", type=Path)
    arguments = parser.parse_args()
    try:
        summary = validate_qualification_set(load_qualification_set(arguments.evidence))
    except (OSError, ValueError) as error:
        print(f"training qualification evidence failed: {error}", file=sys.stderr)
        return 1
    print(
        "training qualification evidence passed: "
        f"{summary.successful_runs}/{summary.eligible_runs} eligible runs"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
