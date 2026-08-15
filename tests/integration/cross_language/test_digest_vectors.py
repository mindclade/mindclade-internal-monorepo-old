# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import json
import re
from pathlib import Path

F = Path(__file__).parent / "fixtures" / "primitives_v1.json"


def test_digest_fixture_is_canonical_sha256():
    digest = json.loads(F.read_text())["digest"]
    assert re.fullmatch(r"sha256:[0-9a-f]{64}", digest)
