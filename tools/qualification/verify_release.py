# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Offline CLI for producer-side release evidence."""

from __future__ import annotations

import argparse
from pathlib import Path

from tools.qualification.evidence import evidence_digest, validate_file


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("evidence", type=Path)
    parser.add_argument(
        "--schema",
        type=Path,
        default=Path(__file__).resolve().parent / "schemas/release-evidence.schema.json",
    )
    args = parser.parse_args()
    try:
        value, errors = validate_file(args.evidence, args.schema)
    except (OSError, ValueError) as exc:
        print(f"ERROR: {exc}")
        return 1
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print(f"release evidence validation passed ({evidence_digest(value)})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
