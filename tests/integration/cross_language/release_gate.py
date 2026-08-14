#!/usr/bin/env python3
"""Cross-language compatibility release gate."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def main() -> int:
    tests = [
        "tests/integration/cross_language/test_digest_vectors.py",
        "tests/integration/cross_language/test_identifiers.py",
        "tests/integration/cross_language/test_resource_versions.py",
        "tests/integration/cross_language/test_execution_ticket_golden.py",
        "tests/integration/cross_language/test_event_envelopes.py",
        "tests/integration/cross_language/test_worker_protocol.py",
        "tests/integration/cross_language/test_wire_compatibility.py",
    ]
    return subprocess.call([sys.executable, "-m", "pytest", "-q", *tests], cwd=ROOT)


if __name__ == "__main__":
    raise SystemExit(main())
