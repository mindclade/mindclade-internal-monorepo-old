#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def check(root: Path) -> list[str]:
    errors = []
    affected = (root / "ci/common/affected.py").read_text(errors="replace")
    pipeline = (root / "ci/presubmit/pipeline.py").read_text(errors="replace")
    workflow = (root / ".github/workflows/presubmit.yml").read_text(errors="replace")
    for token in (
        "def select(",
        "def git_changed(",
        "def rust_qualification_required(",
        "GLOBAL_PREFIXES",
        "DEP_ATTRIBUTES",
    ):
        if token not in affected:
            errors.append(f"affected selector missing {token}")
    for token in (
        "affected.select(changed)",
        "affected.rust_qualification_required(changed)",
    ):
        if token not in pipeline:
            errors.append(f"presubmit pipeline missing {token}")
    # The requirement is that the pipeline shells out to bazelw with the test verb. This used to
    # be spelled as the literal substring `bazelw'),'test'`, which encoded one exact quoting and
    # spacing of that call — single quotes, no space after the comma. The pipeline has never been
    # written that way, so the check reported a missing token on a file that satisfied it, and
    # would break again the first time a formatter touched the line. Matched on structure now.
    if not re.search(r"""bazelw["']\s*\)\s*,\s*["']test["']""", pipeline):
        errors.append("presubmit pipeline must invoke bazelw with the test verb")
    if "fetch-depth: 0" not in workflow or "ci/presubmit/pipeline.py" not in workflow:
        errors.append("presubmit workflow must use full history and affected pipeline")
    return errors


def main() -> int:
    errors = check(ROOT)
    [print(e) for e in errors]
    if errors:
        return 1
    print("affected presubmit contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
