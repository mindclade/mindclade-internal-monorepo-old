# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Generate the AsyncAPI projection from the canonical event catalog."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from protocols.events.validate_contracts import load_json_yaml, render_asyncapi  # noqa: E402

CATALOG = ROOT / "protocols/events/catalog.yaml"
ASYNCAPI = ROOT / "protocols/events/asyncapi.yaml"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="fail when AsyncAPI has drift")
    args = parser.parse_args()
    expected = render_asyncapi(load_json_yaml(CATALOG))
    if args.check:
        actual = ASYNCAPI.read_text(encoding="utf-8") if ASYNCAPI.is_file() else ""
        if actual != expected:
            print("protocols/events/asyncapi.yaml has generated drift")
            return 1
        print("event catalog generation is deterministic and current")
        return 0
    ASYNCAPI.write_text(expected, encoding="utf-8")
    print("generated protocols/events/asyncapi.yaml")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
