from __future__ import annotations

import json
import re
from pathlib import Path

F = Path(__file__).parent / "fixtures" / "primitives_v1.json"


def test_resource_version_binds_generation_and_digest():
    data = json.loads(F.read_text())
    value = data["resource_version"]
    m = re.fullmatch(r"rv1:([1-9][0-9]*):(sha256:[0-9a-f]{64})", value)
    assert m
    assert int(m.group(1)) == 42
    assert m.group(2) == data["digest"]
