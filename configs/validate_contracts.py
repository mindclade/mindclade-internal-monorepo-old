# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate every canonical configuration schema and its safe fixture."""

from __future__ import annotations

from pathlib import Path

from configs.contract_validation import validate_catalog


def main() -> int:
    root = Path(__file__).resolve().parent
    errors = validate_catalog(root)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print("configuration contract validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
