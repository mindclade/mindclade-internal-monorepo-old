#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Fail-closed static gate for MLOps configuration, alert, and release contracts."""

from __future__ import annotations

import sys
from pathlib import Path

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
if str(REPOSITORY_ROOT) not in sys.path:
    sys.path.insert(0, str(REPOSITORY_ROOT))

from configs.contract_validation import load_json  # noqa: E402
from configs.contract_validation import validate_catalog as validate_config_catalog  # noqa: E402
from infra.observability.alert_contracts import (  # noqa: E402
    validate_catalog as validate_alert_catalog,
)
from tools.qualification.evidence import load_evidence, validate_evidence  # noqa: E402


def check(root: Path) -> list[str]:
    errors = [f"configs: {error}" for error in validate_config_catalog(root / "configs")]
    errors.extend(
        f"observability: {error}"
        for error in validate_alert_catalog(root / "infra" / "observability")
    )
    evidence_root = root / "tools" / "qualification"
    try:
        evidence = load_evidence(evidence_root / "fixtures" / "release-evidence.valid.json")
        schema = load_json(evidence_root / "schemas" / "release-evidence.schema.json")
        errors.extend(f"release evidence: {error}" for error in validate_evidence(evidence, schema))
    except (OSError, ValueError) as exc:
        errors.append(f"release evidence: {exc}")
    return sorted(set(errors))


def main() -> int:
    errors = check(REPOSITORY_ROOT)
    for error in errors:
        print(error)
    if errors:
        return 1
    print("MLOps static contract validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
